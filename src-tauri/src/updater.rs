use crate::{hotkey, sidecar::Engine};
use serde_json::json;
use std::{
    sync::atomic::{AtomicBool, Ordering},
    time::Duration,
};
use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
use tauri_plugin_updater::UpdaterExt;

#[derive(Default)]
pub struct UpdateState {
    busy: AtomicBool,
    startup_checked: AtomicBool,
}

struct Checking<'a>(&'a AtomicBool);

impl Drop for Checking<'_> {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

pub fn on_engine_ready(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let Some(engine) = app.try_state::<Engine>() else {
            return;
        };
        let mut settings = engine.request("settings.get", json!({})).await;
        for _ in 0..5 {
            if !matches!(&settings, Err(error) if error == "引擎请求队列不可用")
                || engine.is_exiting()
            {
                break;
            }
            let _ = tauri::async_runtime::spawn_blocking(|| {
                std::thread::sleep(Duration::from_millis(40));
            })
            .await;
            settings = engine.request("settings.get", json!({})).await;
        }
        let Ok(settings) = settings else {
            return;
        };
        let state = app.state::<UpdateState>();
        if state.startup_checked.swap(true, Ordering::AcqRel) {
            return;
        }
        if settings["updater"]["checkOnStartup"] == true && !engine.is_exiting() {
            if let Err(error) = check(&app).await {
                eprintln!("startup updater: {error}");
            }
        }
    });
}

pub async fn check(app: &AppHandle) -> Result<&'static str, String> {
    let state = app.state::<UpdateState>();
    if state.busy.swap(true, Ordering::AcqRel) {
        return Ok("busy");
    }
    let _checking = Checking(&state.busy);
    if app.state::<Engine>().is_exiting() {
        return Err("应用正在退出，请重新打开后检查更新".into());
    }
    let before_exit = app.clone();
    let updater = app
        .updater_builder()
        .timeout(Duration::from_secs(60))
        .on_before_exit(move || {
            before_exit.state::<Engine>().stop_for_update();
        })
        .build()
        .map_err(|error| format!("无法初始化更新：{error}"))?;
    let Some(update) = updater
        .check()
        .await
        .map_err(|error| format!("检查更新失败：{error}"))?
    else {
        return Ok("current");
    };

    let (send, receive) = std::sync::mpsc::sync_channel(1);
    let dialog = app
        .dialog()
        .message(format!(
            "发现版本 {}。安装时 Easy Command 将退出，正在运行的脚本也会结束。现在下载并安装？",
            update.version
        ))
        .title("Easy Command 更新")
        .buttons(MessageDialogButtons::YesNo);
    let dialog = if let Some(window) = app
        .get_webview_window("main")
        .filter(|w| w.is_visible().unwrap_or(false))
    {
        dialog.parent(&window)
    } else {
        dialog
    };
    dialog.show(move |confirmed| {
        let _ = send.send(confirmed);
    });
    let confirmed = tauri::async_runtime::spawn_blocking(move || receive.recv().unwrap_or(false))
        .await
        .map_err(|error| error.to_string())?;
    if !confirmed {
        return Ok("cancelled");
    }

    hotkey::notify(app, "正在下载并校验更新");
    let bytes = update
        .download(|_, _| {}, || {})
        .await
        .map_err(|error| format!("下载或签名校验失败：{error}"))?;
    if app.state::<Engine>().is_exiting() {
        return Err("应用正在退出，已停止安装更新".into());
    }
    let result = tauri::async_runtime::spawn_blocking(move || update.install(bytes))
        .await
        .map_err(|error| error.to_string())?;
    if let Err(error) = result {
        if app.state::<Engine>().exit_ready() {
            return Err(format!(
                "安装程序未启动：{error}。请退出并重新打开 Easy Command。"
            ));
        }
        return Err(format!("安装更新失败：{error}"));
    }
    Ok("installed")
}
