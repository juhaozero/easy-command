use crate::sidecar::Engine;
use serde::Deserialize;
use serde_json::{json, Value};
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Mutex,
};
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons, MessageDialogKind};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut, ShortcutState};
use tauri_plugin_notification::NotificationExt;

#[derive(Default)]
pub struct Hotkeys {
    registered: Mutex<Option<Shortcut>>,
    refreshing: Mutex<()>,
    triggering: AtomicBool,
}

#[derive(Deserialize)]
struct Config {
    enabled: bool,
    accelerator: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Target {
    status: String,
    script_id: String,
    name: String,
    dangerous: Option<bool>,
}

pub fn notify(app: &AppHandle, message: &str) {
    let _ = app.emit("automation://notice", message);
    if let Err(error) = app
        .notification()
        .builder()
        .title("Easy Command")
        .body(message)
        .show()
    {
        eprintln!("notification: {error}");
    }
}

pub fn refresh(app: AppHandle) {
    tauri::async_runtime::spawn_blocking(move || {
        let Some(state) = app.try_state::<Hotkeys>() else {
            return;
        };
        let _serial = state.refreshing.lock().unwrap();
        let Some(engine) = app.try_state::<Engine>() else {
            return;
        };
        if engine.is_exiting() {
            return;
        }
        let mut response =
            tauri::async_runtime::block_on(engine.request("settings.get", json!({})));
        for _ in 0..5 {
            if !matches!(&response, Err(error) if error == "引擎请求队列不可用")
                || engine.is_exiting()
            {
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(40));
            response = tauri::async_runtime::block_on(engine.request("settings.get", json!({})));
        }
        let result = response.and_then(|value| {
            let config: Config =
                serde_json::from_value(value["hotkey"].clone()).map_err(|e| e.to_string())?;
            let next = if config.enabled && !config.accelerator.is_empty() {
                Some(
                    config
                        .accelerator
                        .parse::<Shortcut>()
                        .map_err(|e| e.to_string())?,
                )
            } else {
                None
            };
            let mut old = state.registered.lock().unwrap();
            if *old == next {
                return Ok(());
            }
            if let Some(next) = next {
                app.global_shortcut()
                    .register(next)
                    .map_err(|e| e.to_string())?;
            }
            if let Some(previous) = *old {
                if let Err(error) = app.global_shortcut().unregister(previous) {
                    if let Some(next) = next {
                        let _ = app.global_shortcut().unregister(next);
                    }
                    return Err(error.to_string());
                }
            }
            *old = next;
            Ok(())
        });
        if let Err(error) = result {
            notify(
                &app,
                &format!("快捷键配置未生效，请到设置更换组合键。{error}"),
            );
        }
    });
}

pub fn pressed(app: &AppHandle, shortcut: &Shortcut, state: ShortcutState) {
    if state != ShortcutState::Pressed {
        return;
    }
    let app = app.clone();
    let shortcut = *shortcut;
    tauri::async_runtime::spawn(async move {
        if app.state::<Hotkeys>().registered.lock().unwrap().as_ref() == Some(&shortcut) {
            let _ = trigger(&app).await;
        }
    });
}

pub async fn trigger(app: &AppHandle) -> Result<Value, String> {
    let state = app.state::<Hotkeys>();
    if state.triggering.swap(true, Ordering::AcqRel) {
        return Ok(json!({"status":"busy"}));
    }
    let result = run(app).await;
    state.triggering.store(false, Ordering::Release);
    if let Err(error) = &result {
        notify(app, error);
    }
    result
}

async fn run(app: &AppHandle) -> Result<Value, String> {
    let engine = app.state::<Engine>();
    if engine.is_exiting() {
        return Err("应用正在退出".into());
    }
    let target: Target = serde_json::from_value(engine.request("hotkey.trigger", json!({})).await?)
        .map_err(|e| e.to_string())?;
    let confirmed = match target.status.as_str() {
        "ready" if target.dangerous == Some(false) => false,
        "needs_confirm" => {
            let (send, recv) = std::sync::mpsc::sync_channel(1);
            app.dialog()
                .message(format!("确定运行危险脚本「{}」？", target.name))
                .title("确认运行危险脚本")
                .kind(MessageDialogKind::Warning)
                .buttons(MessageDialogButtons::YesNo)
                .show(move |confirmed| {
                    let _ = send.send(confirmed);
                });
            let accepted =
                tauri::async_runtime::spawn_blocking(move || recv.recv().unwrap_or(false))
                    .await
                    .map_err(|e| e.to_string())?;
            if !accepted {
                let _ = app.emit("automation://notice", "已取消快捷键运行");
                return Ok(json!({"status":"cancelled"}));
            }
            true
        }
        _ => return Err("快捷键预检响应无效".into()),
    };
    if engine.is_exiting() {
        return Err("应用正在退出".into());
    }
    engine
        .request(
            "script.run",
            json!({
                "scriptId":target.script_id,"trigger":"hotkey","confirmed":confirmed
            }),
        )
        .await
}
