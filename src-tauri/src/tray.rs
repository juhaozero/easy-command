use crate::{hotkey, sidecar::Engine};
use serde_json::{json, Value};
use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager,
};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons, MessageDialogKind};

pub struct TrayState {
    schedule: MenuItem<tauri::Wry>,
    closing: AtomicBool,
    running_recent: AtomicBool,
    changing_schedule: AtomicBool,
}

pub fn setup(app: &AppHandle) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "tray-open", "打开主界面", true, None::<&str>)?;
    let recent = MenuItem::with_id(app, "tray-recent", "运行最近脚本", true, None::<&str>)?;
    let schedule = MenuItem::with_id(app, "tray-schedule", "暂停定时任务", false, None::<&str>)?;
    let exit = MenuItem::with_id(app, "tray-exit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &recent, &schedule, &exit])?;
    let mut builder = TrayIconBuilder::with_id("main-tray")
        .menu(&menu)
        .tooltip("Easy Command")
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show(tray.app_handle());
            }
        })
        .on_menu_event(|app, event| match event.id.as_ref() {
            "tray-open" => show(app),
            "tray-recent" => {
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    let _ = run_last(&app).await;
                });
            }
            "tray-schedule" => {
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    toggle_schedule(&app).await;
                });
            }
            "tray-exit" => app.exit(0),
            _ => {}
        });
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    app.manage(TrayState {
        schedule,
        closing: AtomicBool::new(false),
        running_recent: AtomicBool::new(false),
        changing_schedule: AtomicBool::new(false),
    });
    Ok(())
}

pub fn show(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

pub fn close(app: &AppHandle, api: &tauri::CloseRequestApi) {
    api.prevent_close();
    let engine = app.state::<Engine>();
    if engine.exit_ready() {
        app.exit(0);
        return;
    }
    if engine.is_exiting() {
        return;
    }
    let Some(tray) = app.try_state::<TrayState>() else {
        engine.begin_exit(app.clone());
        return;
    };
    if tray.closing.swap(true, Ordering::AcqRel) {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        close_window(&app).await;
        app.state::<TrayState>()
            .closing
            .store(false, Ordering::Release);
    });
}

async fn close_window(app: &AppHandle) {
    let engine = app.state::<Engine>();
    let Ok(settings) = read(&engine, "settings.get").await else {
        engine.begin_exit(app.clone());
        return;
    };
    if settings["closeToTray"] != true {
        engine.begin_exit(app.clone());
        return;
    }
    if settings["firstCloseTipShown"] != true {
        let dialog = app.dialog().message("关闭窗口后 Easy Command 仍在通知区域运行。要完全退出，请打开托盘菜单并选择「退出」。")
            .title("Easy Command 仍会运行")
            .buttons(MessageDialogButtons::Ok);
        let dialog = if let Some(window) = app.get_webview_window("main") {
            dialog.parent(&window)
        } else {
            dialog
        };
        wait_dialog(dialog).await;
        if engine.is_exiting() {
            return;
        }
        if let Err(error) = engine
            .request("settings.set", json!({"firstCloseTipShown":true}))
            .await
        {
            hotkey::notify(app, &format!("无法保存首次关窗提示：{error}"));
        }
    }
    if engine.is_exiting() {
        return;
    }
    if let Some(window) = app.get_webview_window("main") {
        if window.hide().is_err() {
            engine.begin_exit(app.clone());
        }
    }
}

async fn wait_dialog<R: tauri::Runtime>(
    dialog: tauri_plugin_dialog::MessageDialogBuilder<R>,
) -> bool {
    let (send, recv) = std::sync::mpsc::sync_channel(1);
    dialog.show(move |value| {
        let _ = send.send(value);
    });
    tauri::async_runtime::spawn_blocking(move || recv.recv().unwrap_or(false))
        .await
        .unwrap_or(false)
}

pub async fn run_last(app: &AppHandle) -> Result<Value, String> {
    let Some(tray) = app.try_state::<TrayState>() else {
        return Err("托盘不可用".into());
    };
    if tray.running_recent.swap(true, Ordering::AcqRel) {
        return Ok(json!({"status":"busy"}));
    }
    let result = recent(app).await;
    tray.running_recent.store(false, Ordering::Release);
    if let Err(error) = &result {
        hotkey::notify(app, error);
    }
    result
}

async fn recent(app: &AppHandle) -> Result<Value, String> {
    let engine = app.state::<Engine>();
    if engine.is_exiting() {
        return Err("应用正在退出".into());
    }
    let error = match engine.request_with_details("tray.runLast", json!({})).await {
        Ok(result) => return Ok(result),
        Err(error) if error.code == "NEEDS_DANGEROUS_CONFIRM" => error,
        Err(error) => return Err(error.to_string()),
    };
    let id = error.details["scriptId"]
        .as_str()
        .ok_or("最近脚本响应缺少 ID")?;
    let name = error.details["name"]
        .as_str()
        .ok_or("最近脚本响应缺少名称")?;
    let dialog = app
        .dialog()
        .message(format!("确定运行危险脚本「{name}」？"))
        .title("确认运行危险脚本")
        .kind(MessageDialogKind::Warning)
        .buttons(MessageDialogButtons::YesNo);
    if !wait_dialog(dialog).await {
        return Ok(json!({"status":"cancelled"}));
    }
    if engine.is_exiting() {
        return Err("应用正在退出".into());
    }
    engine
        .request(
            "script.run",
            json!({"scriptId":id,"trigger":"tray","confirmed":true}),
        )
        .await
}

pub fn refresh_schedule(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let Some(tray) = app.try_state::<TrayState>() else {
            return;
        };
        let Some(engine) = app.try_state::<Engine>() else {
            return;
        };
        if let Ok(paused) = schedule_state(&engine).await {
            update_schedule(&tray, paused);
        }
    });
}

async fn toggle_schedule(app: &AppHandle) {
    let tray = app.state::<TrayState>();
    if tray.changing_schedule.swap(true, Ordering::AcqRel) {
        return;
    }
    let engine = app.state::<Engine>();
    let result = async {
        let paused = schedule_state(&engine).await?;
        let action = if paused {
            "sched.resumeAll"
        } else {
            "sched.pauseAll"
        };
        let state = engine.request(action, json!({})).await?;
        state["paused"]
            .as_bool()
            .ok_or("调度状态响应无效".to_owned())
    }
    .await;
    match result {
        Ok(paused) => update_schedule(&tray, paused),
        Err(error) => hotkey::notify(app, &error),
    }
    tray.changing_schedule.store(false, Ordering::Release);
}

async fn schedule_state(engine: &Engine) -> Result<bool, String> {
    read(engine, "sched.list").await?["paused"]
        .as_bool()
        .ok_or("调度状态响应无效".into())
}

async fn read(engine: &Engine, action: &'static str) -> Result<Value, String> {
    let mut result = engine.request(action, json!({})).await;
    for _ in 0..5 {
        if !matches!(&result, Err(error) if error == "引擎请求队列不可用") || engine.is_exiting()
        {
            break;
        }
        let _ = tauri::async_runtime::spawn_blocking(|| {
            std::thread::sleep(std::time::Duration::from_millis(40));
        })
        .await;
        result = engine.request(action, json!({})).await;
    }
    result
}

fn update_schedule(tray: &TrayState, paused: bool) {
    let _ = tray.schedule.set_text(if paused {
        "恢复定时任务"
    } else {
        "暂停定时任务"
    });
    let _ = tray.schedule.set_enabled(true);
}
