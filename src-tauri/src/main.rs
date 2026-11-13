#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;
mod sidecar;

use sidecar::Engine;
use tauri::{Manager, RunEvent};

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .setup(|app| {
            app.manage(Engine::start(app.handle().clone())?);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::engine_status,
            commands::engine_ping,
            commands::engine_shutdown,
            commands::script_list,
            commands::script_get,
            commands::script_import,
            commands::script_update_meta,
            commands::script_update_content,
            commands::script_delete,
        ])
        .build(tauri::generate_context!())
        .expect("failed to build Easy Command")
        .run(|app, event| {
            if let RunEvent::ExitRequested { api, .. } = event {
                let engine = app.state::<Engine>();
                if !engine.exit_ready() {
                    api.prevent_exit();
                    engine.begin_exit(app.clone());
                }
            }
        });
}
