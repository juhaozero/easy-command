#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;
mod hotkey;
mod sidecar;
mod tray;
mod updater;

use sidecar::Engine;
use tauri::{Manager, RunEvent};

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            tray::show(app)
        }))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, shortcut, event| hotkey::pressed(app, shortcut, event.state()))
                .build(),
        )
        .setup(|app| {
            app.manage(hotkey::Hotkeys::default());
            app.manage(updater::UpdateState::default());
            if let Err(error) = tray::setup(app.handle()) {
                eprintln!("tray setup: {error}");
            }
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
            commands::script_import_dir,
            commands::script_update_meta,
            commands::script_update_content,
            commands::script_delete,
            commands::script_run,
            commands::script_cancel,
            commands::bash_detect,
            commands::bash_set_path,
            commands::history_list,
            commands::history_get,
            commands::settings_get,
            commands::settings_set,
            commands::secret_set,
            commands::secret_clear,
            commands::sched_list,
            commands::sched_upsert,
            commands::sched_delete,
            commands::sched_pause_all,
            commands::sched_resume_all,
            commands::ui_set_selected,
            commands::ui_get_state,
            commands::hotkey_trigger,
            commands::tray_run_last,
            commands::check_update,
        ])
        .build(tauri::generate_context!())
        .expect("failed to build Easy Command")
        .run(|app, event| {
            if let RunEvent::WindowEvent {
                label,
                event: tauri::WindowEvent::CloseRequested { api, .. },
                ..
            } = &event
            {
                if label == "main" {
                    tray::close(app, api);
                }
            }
            if let RunEvent::ExitRequested { api, .. } = event {
                let engine = app.state::<Engine>();
                if !engine.exit_ready() {
                    api.prevent_exit();
                    engine.begin_exit(app.clone());
                }
            }
        });
}
