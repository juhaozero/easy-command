mod process;
mod protocol;

use process::Process;
use protocol::{encode_request, read_message, Incoming};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    io::{self, BufReader, Write},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU8, Ordering},
        mpsc::{self, Receiver, SyncSender},
        Arc, Mutex,
    },
    thread::{self, JoinHandle},
    time::{Duration, Instant},
};
use tauri::{AppHandle, Emitter};
use uuid::Uuid;

const HELLO_TIMEOUT: Duration = Duration::from_secs(5);
const REQUEST_TIMEOUT: Duration = Duration::from_secs(3);
const SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(2);
const INITIAL_DELAY: Duration = Duration::from_millis(500);
const MAX_DELAY: Duration = Duration::from_secs(8);
const POLL: Duration = Duration::from_millis(20);
const RUNNING: u8 = 0;
const EXITING: u8 = 1;
const EXIT_READY: u8 = 2;

type Reply = Result<Value, RequestError>;

pub struct RequestError {
    pub code: String,
    pub message: String,
    pub details: Value,
}

impl From<String> for RequestError {
    fn from(message: String) -> Self {
        Self {
            code: String::new(),
            message,
            details: Value::Null,
        }
    }
}

impl From<&str> for RequestError {
    fn from(message: &str) -> Self {
        message.to_owned().into()
    }
}

impl std::fmt::Display for RequestError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if self.code.is_empty() {
            f.write_str(&self.message)
        } else {
            write!(f, "{}：{}", self.code, self.message)
        }
    }
}

struct Request {
    action: &'static str,
    id: Uuid,
    line: Vec<u8>,
    reply: SyncSender<Reply>,
}

struct Pending {
    id: Uuid,
    action: &'static str,
    reply: Option<SyncSender<Reply>>,
    deadline: Instant,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    revision: u64,
    phase: &'static str,
    message: String,
    pid: Option<u32>,
    version: Option<String>,
    data_dir: Option<String>,
    retry_ms: Option<u64>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Hello {
    version: String,
    data_dir: String,
}

struct Shared {
    status: Mutex<Status>,
    exit: AtomicU8,
}

pub struct Engine {
    requests: SyncSender<Request>,
    shared: Arc<Shared>,
    worker: Mutex<Option<JoinHandle<()>>>,
}

impl Engine {
    pub fn start(app: AppHandle) -> io::Result<Self> {
        let binary = std::env::current_exe()?.with_file_name("easy-command-engine.exe");
        let shared = Arc::new(Shared {
            status: Mutex::new(Status {
                revision: 0,
                phase: "starting",
                message: "正在启动引擎".into(),
                pid: None,
                version: None,
                data_dir: None,
                retry_ms: None,
            }),
            exit: AtomicU8::new(RUNNING),
        });
        let (requests, incoming) = mpsc::sync_channel(1);
        let worker_state = shared.clone();
        let worker = thread::Builder::new()
            .name("sidecar-supervisor".into())
            .spawn(move || supervise(&app, &worker_state, &binary, &incoming))?;
        Ok(Self {
            requests,
            shared,
            worker: Mutex::new(Some(worker)),
        })
    }

    pub fn status(&self) -> Status {
        self.shared.status.lock().unwrap().clone()
    }

    pub async fn request(&self, action: &'static str, payload: Value) -> Result<Value, String> {
        self.request_with_details(action, payload)
            .await
            .map_err(|e| e.to_string())
    }

    pub async fn request_with_details(&self, action: &'static str, payload: Value) -> Reply {
        let id = Uuid::new_v4();
        let line = encode_request(id, action, payload)?;
        let (reply, result) = mpsc::sync_channel(1);
        self.requests
            .try_send(Request {
                action,
                id,
                line,
                reply,
            })
            .map_err(|_| "引擎请求队列不可用".to_string())?;
        tauri::async_runtime::spawn_blocking(move || {
            result
                .recv_timeout(Duration::from_secs(7))
                .map_err(|_| RequestError::from("引擎未响应"))?
        })
        .await
        .map_err(|error| error.to_string())?
    }

    pub fn exit_ready(&self) -> bool {
        self.shared.exit.load(Ordering::Acquire) == EXIT_READY
    }

    pub fn is_exiting(&self) -> bool {
        self.shared.exiting()
    }

    pub fn begin_exit(&self, app: AppHandle) {
        if self
            .shared
            .exit
            .compare_exchange(RUNNING, EXITING, Ordering::AcqRel, Ordering::Acquire)
            .is_err()
        {
            return;
        }
        let worker = self.worker.lock().unwrap().take();
        let shared = self.shared.clone();
        tauri::async_runtime::spawn_blocking(move || {
            if let Some(worker) = worker {
                let _ = worker.join();
            }
            shared.exit.store(EXIT_READY, Ordering::Release);
            app.exit(0);
        });
    }

    // The Windows updater exits the process directly after launching the installer.
    // Its hook must finish Sidecar shutdown synchronously before returning.
    pub fn stop_for_update(&self) {
        if self
            .shared
            .exit
            .compare_exchange(RUNNING, EXITING, Ordering::AcqRel, Ordering::Acquire)
            .is_ok()
        {
            if let Some(worker) = self.worker.lock().unwrap().take() {
                let _ = worker.join();
            }
            self.shared.exit.store(EXIT_READY, Ordering::Release);
        } else {
            while !self.exit_ready() {
                thread::sleep(Duration::from_millis(20));
            }
        }
    }
}

impl Shared {
    fn exiting(&self) -> bool {
        self.exit.load(Ordering::Acquire) != RUNNING
    }

    fn publish(
        &self,
        app: &AppHandle,
        phase: &'static str,
        message: impl Into<String>,
        pid: Option<u32>,
        hello: Option<Hello>,
        retry: Option<Duration>,
    ) {
        let mut status = self.status.lock().unwrap();
        let (version, data_dir) = hello.map_or((None, None), |hello| {
            (Some(hello.version), Some(hello.data_dir))
        });
        *status = Status {
            revision: status.revision + 1,
            phase,
            message: message.into(),
            pid,
            version,
            data_dir,
            retry_ms: retry.map(|delay| delay.as_millis() as u64),
        };
        let event = status.clone();
        drop(status);
        let _ = app.emit("engine://status", event);
    }
}

fn supervise(app: &AppHandle, shared: &Shared, binary: &Path, requests: &Receiver<Request>) {
    let mut delay = INITIAL_DELAY;
    let mut was_ready = false;
    while !shared.exiting() {
        shared.publish(app, "starting", "正在启动引擎", None, None, None);
        let mut ready_at = None;
        let result = Process::spawn(binary)
            .map_err(|error| format!("启动引擎失败：{error}"))
            .and_then(|(process, mut stdin, stdout, stderr)| {
                let (input, outgoing) = mpsc::sync_channel::<Vec<u8>>(1);
                let writer = thread::spawn(move || {
                    while let Ok(line) = outgoing.recv() {
                        if stdin.write_all(&line).is_err() {
                            break;
                        }
                    }
                });
                // Stderr stays outside the protocol and is drained independently of stdout.
                let stderr_reader = thread::spawn(move || {
                    let _ = io::copy(&mut BufReader::new(stderr), &mut io::stderr());
                });
                let (send, messages) = mpsc::sync_channel(16);
                let reader = thread::spawn(move || {
                    let mut stdout = BufReader::new(stdout);
                    loop {
                        let message = read_message(&mut stdout);
                        let terminal = !matches!(message, Ok(Some(_)));
                        if send.send(message).is_err() || terminal {
                            break;
                        }
                    }
                });
                let mut session = Session {
                    app,
                    shared,
                    requests,
                    messages,
                    input,
                    pid: process.pid,
                    ready_at: &mut ready_at,
                    pending: None,
                    shutdown_deadline: None,
                    recovering: was_ready,
                };
                let result = session.run();
                drop(session);
                let wait_ms = if result.is_ok() { 100 } else { 0 };
                let exit = process.finish(wait_ms);
                let _ = writer.join();
                let _ = reader.join();
                let _ = stderr_reader.join();
                result.and_then(|requested| {
                    let code = exit.map_err(|error| format!("清理引擎失败：{error}"))?;
                    if code == 0 && requested {
                        Ok(())
                    } else {
                        Err(format!("引擎已退出（退出码 {code}）"))
                    }
                })
            });
        if shared.exiting() {
            break;
        }
        was_ready |= ready_at.is_some();
        if result.is_ok() || ready_at.is_some_and(|at| at.elapsed() >= MAX_DELAY) {
            delay = INITIAL_DELAY;
        }
        let message = result
            .as_ref()
            .err()
            .map_or("引擎已正常关闭", String::as_str);
        shared.publish(app, "backoff", message, None, None, Some(delay));
        let until = Instant::now() + delay;
        while !shared.exiting() && Instant::now() < until {
            if let Ok(request) = requests.recv_timeout(POLL) {
                let _ = request.reply.send(Err("引擎正在等待重启".into()));
            }
        }
        if result.is_err() {
            delay = (delay * 2).min(MAX_DELAY);
        }
    }
}

type IncomingLine = io::Result<Option<Result<Incoming, protocol::ProtocolError>>>;

struct Session<'a> {
    app: &'a AppHandle,
    shared: &'a Shared,
    requests: &'a Receiver<Request>,
    messages: Receiver<IncomingLine>,
    input: SyncSender<Vec<u8>>,
    pid: u32,
    ready_at: &'a mut Option<Instant>,
    pending: Option<Pending>,
    shutdown_deadline: Option<Instant>,
    recovering: bool,
}

impl Session<'_> {
    fn run(&mut self) -> Result<bool, String> {
        let hello_deadline = Instant::now() + HELLO_TIMEOUT;
        loop {
            let ready = self.ready_at.is_some();
            let now = Instant::now();
            if self.shared.exiting() && self.shutdown_deadline.is_none() {
                if !ready {
                    return Err("应用在握手期间退出".into());
                }
                if let Some(pending) = self.pending.take() {
                    if let Some(reply) = pending.reply {
                        let _ = reply.send(Err("应用正在退出".into()));
                    }
                }
                let id = Uuid::new_v4();
                let line = encode_request(id, "system.shutdown", json!({}))?;
                self.send("system.shutdown", id, line, None)?;
            }
            if !ready && now >= hello_deadline {
                return Err("等待 system.hello 超时（5 秒）".into());
            }
            if self
                .shutdown_deadline
                .is_some_and(|deadline| now >= deadline)
            {
                return Err("等待引擎关闭超时（2 秒）".into());
            }
            if self
                .pending
                .as_ref()
                .is_some_and(|pending| now >= pending.deadline)
            {
                return Err("引擎请求超时".into());
            }
            if self.pending.is_none() {
                if let Ok(request) = self.requests.try_recv() {
                    if ready && self.shutdown_deadline.is_none() {
                        self.send(
                            request.action,
                            request.id,
                            request.line,
                            Some(request.reply),
                        )?;
                    } else {
                        let _ = request.reply.send(Err("引擎当前不可用".into()));
                    }
                }
            }

            match self.messages.recv_timeout(POLL) {
                Ok(Ok(Some(Ok(message)))) => self.receive(message),
                Ok(Ok(Some(Err(error)))) => warn(self.app, error.message, error.truncated),
                Ok(Ok(None)) => {
                    return Ok(self.shutdown_deadline.is_some() && self.pending.is_none())
                }
                Ok(Err(error)) => return Err(format!("读取引擎 stdout 失败：{error}")),
                Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(mpsc::RecvTimeoutError::Disconnected) => return Err("引擎读循环中断".into()),
            }
        }
    }

    fn send(
        &mut self,
        action: &'static str,
        id: Uuid,
        line: Vec<u8>,
        reply: Option<SyncSender<Reply>>,
    ) -> Result<(), String> {
        let deadline = Instant::now()
            + if action == "system.shutdown" {
                SHUTDOWN_TIMEOUT
            } else {
                REQUEST_TIMEOUT
            };
        self.input
            .try_send(line)
            .map_err(|error| format!("发送请求失败：{error}"))?;
        self.pending = Some(Pending {
            id,
            action,
            reply,
            deadline,
        });
        if action == "system.shutdown" {
            self.shutdown_deadline = Some(deadline);
            self.shared.publish(
                self.app,
                "stopping",
                "正在关闭引擎",
                Some(self.pid),
                None,
                None,
            );
        }
        Ok(())
    }

    fn receive(&mut self, message: Incoming) {
        match message {
            Incoming::Evt {
                v: 1,
                event,
                payload,
                ..
            } if event == "system.hello" && self.ready_at.is_none() => {
                match serde_json::from_value::<Hello>(payload) {
                    Ok(hello)
                        if !hello.version.trim().is_empty()
                            && PathBuf::from(&hello.data_dir).is_absolute() =>
                    {
                        *self.ready_at = Some(Instant::now());
                        let message = if self.recovering {
                            "引擎已恢复"
                        } else {
                            "引擎已就绪"
                        };
                        self.shared.publish(
                            self.app,
                            "ready",
                            message,
                            Some(self.pid),
                            Some(hello),
                            None,
                        );
                        crate::hotkey::refresh(self.app.clone());
                        crate::tray::refresh_schedule(self.app.clone());
                        crate::updater::on_engine_ready(self.app.clone());
                    }
                    _ => warn(self.app, "system.hello 的 version 或 dataDir 无效", false),
                }
            }
            Incoming::Evt {
                v: 1,
                request_id: None,
                event,
                payload,
            } if event == "system.log" && payload["code"] == "SETTINGS_CHANGED" => {
                if payload["scope"] == "schedule" {
                    crate::tray::refresh_schedule(self.app.clone());
                } else {
                    crate::hotkey::refresh(self.app.clone());
                }
            }
            Incoming::Evt {
                v: 1,
                request_id: None,
                event,
                payload,
            } if event == "sched.fired" => {
                if payload["dangerous"] == true {
                    crate::hotkey::notify(self.app, "已执行危险脚本");
                }
                let _ = self.app.emit(
                    "engine://evt",
                    json!({
                        "v":1,"type":"evt","event":event,"payload":payload
                    }),
                );
            }
            Incoming::Evt {
                v: 1,
                event,
                payload,
                ..
            } if event == "system.protocol_error" => {
                if let (Some("PROTOCOL_ERROR"), Some(message), Some(truncated)) = (
                    payload["code"].as_str(),
                    payload["message"].as_str(),
                    payload["truncated"].as_bool(),
                ) {
                    warn(self.app, message, truncated);
                } else {
                    warn(self.app, "system.protocol_error 载荷无效", false);
                }
            }
            Incoming::Evt {
                v: 1,
                request_id,
                event,
                payload,
            } if matches!(
                event.as_str(),
                "exec.started" | "exec.stdout" | "exec.stderr" | "exec.exit" | "system.log"
            ) && payload["runId"]
                .as_str()
                .is_some_and(|id| Uuid::parse_str(id).is_ok()) =>
            {
                let _ = self.app.emit(
                    "engine://evt",
                    json!({
                        "v": 1, "type": "evt", "requestId": request_id,
                        "event": event, "payload": payload,
                    }),
                );
            }
            Incoming::Res {
                v: 1,
                request_id,
                ok,
                payload,
                error,
            } => {
                let Some(pending) = self.pending.as_ref() else {
                    warn(self.app, "收到没有待处理请求的响应", false);
                    return;
                };
                if Uuid::parse_str(&request_id).ok() != Some(pending.id) {
                    warn(self.app, "响应的 requestId 不匹配", false);
                    return;
                }
                let reply = match (ok, payload, error) {
                    (true, Some(payload), None)
                        if pending.action == "system.ping" && payload["pong"] == true
                            || pending.action == "system.shutdown"
                                && payload.as_object().is_some_and(|p| p.is_empty()) =>
                    {
                        Ok(payload)
                    }
                    (true, Some(payload), None)
                        if (pending.action.starts_with("script.")
                            || pending.action.starts_with("bash.")
                            || pending.action.starts_with("history.")
                            || pending.action.starts_with("settings.")
                            || pending.action.starts_with("secret.")
                            || pending.action.starts_with("sched.")
                            || pending.action.starts_with("ui.")
                            || pending.action.starts_with("hotkey.")
                            || pending.action == "tray.runLast")
                            && payload.is_object() =>
                    {
                        Ok(payload)
                    }
                    (false, None, Some(error))
                        if !error.code.is_empty() && error.details.is_object() =>
                    {
                        Err(RequestError {
                            code: error.code,
                            message: error.message,
                            details: error.details,
                        })
                    }
                    _ => {
                        warn(self.app, "响应载荷与请求不匹配", false);
                        return;
                    }
                };
                if let Some(sender) = self.pending.take().and_then(|pending| pending.reply) {
                    let _ = sender.send(reply);
                }
            }
            _ => warn(self.app, "收到未知、重复或版本无效的协议消息", false),
        }
    }
}

impl Drop for Session<'_> {
    fn drop(&mut self) {
        if let Some(reply) = self.pending.take().and_then(|pending| pending.reply) {
            let _ = reply.send(Err("引擎连接已断开或请求超时".into()));
        }
    }
}

fn warn(app: &AppHandle, message: &str, truncated: bool) {
    use std::io::Write;
    let _ = writeln!(io::stderr(), "PROTOCOL_ERROR: {message}");
    let _ = app.emit(
        "engine://evt",
        json!({
            "v": 1, "type": "evt", "event": "system.protocol_error",
            "payload": { "code": "PROTOCOL_ERROR", "message": message, "truncated": truncated }
        }),
    );
}
