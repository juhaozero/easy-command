package exec

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var createJob = kernel.NewProc("CreateJobObjectW")
var setJob = kernel.NewProc("SetInformationJobObject")
var terminateJob = kernel.NewProc("TerminateJobObject")
var initAttributes = kernel.NewProc("InitializeProcThreadAttributeList")
var updateAttribute = kernel.NewProc("UpdateProcThreadAttribute")
var deleteAttributes = kernel.NewProc("DeleteProcThreadAttributeList")

const (
	killOnClose                 = 0x2000
	jobExtendedLimitInformation = 9
	attributeHandleList         = 0x20002
	attributeJobList            = 0x2000d
	createBreakawayFromJob      = 0x01000000
	createNoWindow              = 0x08000000
	extendedStartupInfoPresent  = 0x00080000
	createUnicodeEnvironment    = 0x00000400
)

type extendedLimits struct {
	perProcessTime, perJobTime     int64
	flags                          uint32
	minWorkingSet, maxWorkingSet   uintptr
	activeProcesses                uint32
	affinity                       uintptr
	priorityClass, schedulingClass uint32
	io                             [6]uint64
	memory                         [4]uintptr
}

type startupInfoEx struct {
	syscall.StartupInfo
	attributes unsafe.Pointer
}

type process struct {
	handle, job    syscall.Handle
	stdout, stderr *os.File
}

func newJob() (syscall.Handle, error) {
	handle, _, err := createJob.Call(0, 0)
	if handle == 0 {
		return 0, err
	}
	limits := extendedLimits{flags: killOnClose}
	ok, _, err := setJob.Call(handle, jobExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(handle))
		return 0, err
	}
	return syscall.Handle(handle), nil
}

func start(path, line, dir string, env []string) (result *process, err error) {
	application, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	command, err := syscall.UTF16FromString(line)
	if err != nil {
		return nil, err
	}
	workdir, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	job, err := newJob()
	if err != nil {
		return nil, err
	}
	defer func() {
		if result == nil {
			syscall.CloseHandle(job)
		}
	}()
	stdout, out, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer out.Close()
	defer func() {
		if result == nil {
			stdout.Close()
		}
	}()
	stderr, errOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer errOut.Close()
	defer func() {
		if result == nil {
			stderr.Close()
		}
	}()
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	defer stdin.Close()

	self, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var handles [3]syscall.Handle
	for i, file := range []*os.File{stdin, out, errOut} {
		err := syscall.DuplicateHandle(self, syscall.Handle(file.Fd()), self, &handles[i], 0, true, syscall.DUPLICATE_SAME_ACCESS)
		runtime.KeepAlive(file)
		if err != nil {
			return nil, err
		}
		defer syscall.CloseHandle(handles[i])
	}

	var size uintptr
	initAttributes.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, fmt.Errorf("could not size process attributes")
	}
	storage := make([]uintptr, (size+unsafe.Sizeof(uintptr(0))-1)/unsafe.Sizeof(uintptr(0)))
	attributes := unsafe.Pointer(&storage[0])
	ok, _, err := initAttributes.Call(uintptr(attributes), 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, err
	}
	jobs := [1]syscall.Handle{job}
	defer func() {
		deleteAttributes.Call(uintptr(attributes))
		runtime.KeepAlive(storage)
		runtime.KeepAlive(handles)
		runtime.KeepAlive(jobs)
	}()
	if ok, _, err := updateAttribute.Call(uintptr(attributes), 0, attributeHandleList, uintptr(unsafe.Pointer(&handles[0])), unsafe.Sizeof(handles), 0, 0); ok == 0 {
		return nil, err
	}
	if ok, _, err := updateAttribute.Call(uintptr(attributes), 0, attributeJobList, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs), 0, 0); ok == 0 {
		return nil, err
	}
	var startup startupInfoEx
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = syscall.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = handles[0], handles[1], handles[2]
	startup.attributes = attributes
	var info syscall.ProcessInformation
	// Break away from the shell Job and attach to this run's Job during creation.
	flags := uint32(extendedStartupInfoPresent | createUnicodeEnvironment | createNoWindow | createBreakawayFromJob)
	if err := syscall.CreateProcess(application, &command[0], nil, nil, true, flags, &block[0], workdir, &startup.StartupInfo, &info); err != nil {
		return nil, err
	}
	syscall.CloseHandle(info.Thread)
	return &process{handle: info.Process, job: job, stdout: stdout, stderr: stderr}, nil
}

func (p *process) terminate() error {
	ok, _, err := terminateJob.Call(uintptr(p.job), 1)
	if ok == 0 {
		return err
	}
	return nil
}
