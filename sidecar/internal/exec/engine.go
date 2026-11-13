package exec

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"easy-command/sidecar/internal/bash"
	"easy-command/sidecar/internal/history"
	"easy-command/sidecar/internal/logretain"
	"easy-command/sidecar/internal/script"
)

type Error struct{ Code, Message string }

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{code, message} }

type Emit func(event string, payload map[string]any)

type Engine struct {
	library    *script.Library
	settings   bash.Settings
	diagnostic io.Writer
	history    *history.History
	mu         sync.Mutex
	runs       map[string]*run
}

type run struct {
	process   *process
	cancelled bool
	scriptID  string
}

func New(library *script.Library, settings bash.Settings, diagnostic io.Writer, records *history.History) *Engine {
	return &Engine{library: library, settings: settings, diagnostic: diagnostic, history: records, runs: make(map[string]*run)}
}

func (e *Engine) Start(scriptID string, values map[string]json.RawMessage) (string, func(Emit), error) {
	return e.StartTriggered(scriptID, values, "manual", false)
}

func (e *Engine) StartTriggered(scriptID string, values map[string]json.RawMessage, trigger string, confirmed bool) (string, func(Emit), error) {
	meta, path, err := e.library.Executable(scriptID)
	if err != nil {
		return "", nil, err
	}
	if meta.Dangerous && (trigger == "hotkey" || trigger == "tray") && !confirmed {
		return "", nil, problem("NEEDS_DANGEROUS_CONFIRM", "危险脚本需要确认")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if trigger == "schedule" {
		for _, current := range e.runs {
			if current.scriptID == scriptID {
				return "", nil, problem("SCRIPT_ALREADY_RUNNING", "脚本正在运行，已跳过定时触发")
			}
		}
	}
	args, env, err := arguments(meta.Params, values, meta.Password)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Dir(path)
	if meta.WorkingDir != "" {
		dir = meta.WorkingDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(path), dir)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return "", nil, problem("WORKDIR_NOT_FOUND", "工作目录不存在或不可用")
		}
	}
	encoding, err := e.settings.OutputEncoding()
	if err != nil {
		return "", nil, err
	}
	codepage, _, _ := getACP.Call()
	if encoding == "utf-8" {
		codepage = 65001
	}

	var executable, line string
	switch meta.Kind {
	case "bat":
		executable = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		if !filepath.IsAbs(executable) {
			return "", nil, problem("START_FAILED", "无法定位 Windows cmd.exe")
		}
		line, err = batchCommand(executable, path, args)
		if err != nil {
			return "", nil, err
		}
	case "sh":
		result, err := e.settings.Detect()
		if err != nil {
			return "", nil, err
		}
		if result.Path == nil {
			return "", nil, problem("BASH_NOT_FOUND", "未找到 Git Bash。请安装 Git for Windows，或在执行台指定安装目录中的 bin\\bash.exe")
		}
		executable = *result.Path
		cmd := osExec.Command(executable, append([]string{filepath.ToSlash(path)}, args...)...)
		for i, value := range cmd.Args {
			cmd.Args[i] = syscall.EscapeArg(value)
		}
		line = strings.Join(cmd.Args, " ")
	}
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return "", nil, err
	}
	idBytes[6] = idBytes[6]&15 | 64
	idBytes[8] = idBytes[8]&63 | 128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", idBytes[:4], idBytes[4:6], idBytes[6:8], idBytes[8:10], idBytes[10:])
	// Cmd.Environ deduplicates case-insensitively on Windows and preserves inherited variables.
	child := osExec.Command(executable)
	child.Dir = dir
	child.Env = append(os.Environ(), env...)
	proc, err := start(executable, line, dir, child.Environ())
	if err != nil {
		e.log("create process", err)
		return "", nil, problem("START_FAILED", "无法启动脚本进程")
	}
	current := &run{process: proc, scriptID: scriptID}
	e.runs[id] = current
	started := time.Now()
	record := history.Record{
		ID: id, ScriptID: meta.ID, ScriptName: meta.Name, StartedAt: started, Status: "running",
		Trigger: trigger, ArgsMasked: history.Snapshot(meta.Params, values),
		LogRef: "exec-" + started.Local().Format(logretain.DateLayout) + ".log",
	}
	hideOutput := false
	for _, param := range meta.Params {
		hideOutput = hideOutput || param.Type == "password"
	}
	return id, func(send Emit) { go e.collect(record, current, codepage, hideOutput, send) }, nil
}

func batchCommand(executable, path string, args []string) (string, error) {
	parts := make([]string, 0, len(args)+1)
	for _, value := range append([]string{path}, args...) {
		if strings.ContainsAny(value, "\"%\r\n\x00") {
			return "", problem("INVALID_PARAM", "批处理路径和参数不能包含双引号、百分号或换行")
		}
		parts = append(parts, `"`+value+`"`)
	}
	return syscall.EscapeArg(executable) + ` /d /v:off /s /c "` + strings.Join(parts, " ") + `"`, nil
}

func (e *Engine) Cancel(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.runs[id]
	if current == nil {
		return problem("RUN_NOT_FOUND", "执行不存在或已结束")
	}
	if current.cancelled {
		return nil
	}
	if state, err := syscall.WaitForSingleObject(current.process.handle, 0); err != nil {
		return err
	} else if state == syscall.WAIT_OBJECT_0 {
		return problem("RUN_ALREADY_FINISHED", "执行已结束")
	}
	if err := current.process.terminate(); err != nil {
		return err
	}
	current.cancelled = true
	return nil
}

func (e *Engine) collect(record history.Record, current *run, codepage uintptr, hideOutput bool, send Emit) {
	id := record.ID
	emit := func(event string, payload map[string]any) {
		payload["runId"] = id
		payload["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
		send(event, payload)
	}
	proc := current.process
	emit("exec.started", map[string]any{})
	var notice sync.Once
	report := func(err error) {
		if err != nil {
			notice.Do(func() {
				e.log("history", err)
				emit("system.log", map[string]any{"code": "HISTORY_WRITE_FAILED", "message": "历史未保存"})
			})
		}
	}
	if e.history != nil {
		report(e.history.Begin(record))
	}
	if hideOutput {
		const notice = "此运行包含密码参数，脚本输出已隐藏"
		if e.history != nil {
			report(e.history.Output(record, "stdout", notice, false))
		}
		emit("exec.stdout", map[string]any{"line": notice, "truncated": false})
	}
	var readers sync.WaitGroup
	for event, pipe := range map[string]*os.File{"exec.stdout": proc.stdout, "exec.stderr": proc.stderr} {
		readers.Add(1)
		go func(event string, pipe *os.File) {
			defer readers.Done()
			defer pipe.Close()
			if hideOutput {
				_, _ = io.Copy(io.Discard, pipe)
				return
			}
			var logOutput func(string, bool)
			if e.history != nil {
				logOutput = func(text string, continued bool) {
					report(e.history.Output(record, strings.TrimPrefix(event, "exec."), text, continued))
				}
			}
			if err := streamOutput(pipe, codepage, func(line string, truncated bool) {
				emit(event, map[string]any{"line": line, "truncated": truncated})
			}, logOutput); err != nil {
				e.log("read output", err)
			}
		}(event, pipe)
	}
	_, waitErr := syscall.WaitForSingleObject(proc.handle, syscall.INFINITE)
	var code uint32
	codeErr := syscall.GetExitCodeProcess(proc.handle, &code)
	e.mu.Lock()
	cancelled := current.cancelled
	delete(e.runs, id)
	e.mu.Unlock()
	// The root can exit while descendants still own the output pipes.
	syscall.CloseHandle(proc.job)
	syscall.CloseHandle(proc.handle)
	readers.Wait()
	status := "failed"
	var exitCode any
	switch {
	case cancelled:
		status = "cancelled"
	case waitErr != nil:
		e.log("wait process", waitErr)
	case codeErr != nil:
		e.log("read exit code", codeErr)
	default:
		exitCode = code
		record.ExitCode = &code
		if code == 0 {
			status = "succeeded"
		}
	}
	if e.history != nil {
		ended := time.Now()
		record.EndedAt, record.Status = &ended, status
		report(e.history.Finish(record))
		if err := e.library.MarkRun(record.ScriptID); err != nil {
			e.log("save recent script", err)
		}
	}
	emit("exec.exit", map[string]any{"status": status, "exitCode": exitCode})
}

func (e *Engine) log(action string, err error) {
	if e.diagnostic != nil {
		_, _ = fmt.Fprintf(e.diagnostic, "exec %s: %v\n", action, err)
	}
}
