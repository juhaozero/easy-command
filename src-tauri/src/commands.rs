use crate::sidecar::{Engine, Status};
use serde_json::Value;
use tauri::State;

#[tauri::command]
pub async fn check_update(app: tauri::AppHandle) -> Result<&'static str, String> {
    crate::updater::check(&app).await
}

#[tauri::command]
pub async fn sched_list(engine: State<'_, Engine>) -> Result<Value, String> {
    engine.request("sched.list", serde_json::json!({})).await
}

#[tauri::command]
pub async fn sched_upsert(engine: State<'_, Engine>, rule: Value) -> Result<Value, String> {
    engine
        .request("sched.upsert", serde_json::json!({ "rule": rule }))
        .await
}

#[tauri::command]
pub async fn sched_delete(engine: State<'_, Engine>, rule_id: String) -> Result<Value, String> {
    engine
        .request("sched.delete", serde_json::json!({ "ruleId": rule_id }))
        .await
}

#[tauri::command]
pub async fn sched_pause_all(engine: State<'_, Engine>) -> Result<Value, String> {
    engine
        .request("sched.pauseAll", serde_json::json!({}))
        .await
}

#[tauri::command]
pub async fn sched_resume_all(engine: State<'_, Engine>) -> Result<Value, String> {
    engine
        .request("sched.resumeAll", serde_json::json!({}))
        .await
}

#[tauri::command]
pub async fn ui_set_selected(
    engine: State<'_, Engine>,
    script_id: String,
) -> Result<Value, String> {
    engine
        .request(
            "ui.setSelected",
            serde_json::json!({ "scriptId": script_id }),
        )
        .await
}

#[tauri::command]
pub async fn ui_get_state(engine: State<'_, Engine>) -> Result<Value, String> {
    engine.request("ui.getState", serde_json::json!({})).await
}

#[tauri::command]
pub async fn hotkey_trigger(app: tauri::AppHandle) -> Result<Value, String> {
    crate::hotkey::trigger(&app).await
}

#[tauri::command]
pub async fn secret_set(
    engine: State<'_, Engine>,
    script_id: String,
    param_key: String,
    plaintext: String,
) -> Result<Value, String> {
    engine.request("secret.set", serde_json::json!({ "scriptId": script_id, "paramKey": param_key, "plaintext": plaintext })).await
}

#[tauri::command]
pub async fn secret_clear(
    engine: State<'_, Engine>,
    script_id: String,
    param_key: String,
) -> Result<Value, String> {
    engine
        .request(
            "secret.clear",
            serde_json::json!({ "scriptId": script_id, "paramKey": param_key }),
        )
        .await
}

#[tauri::command]
pub async fn history_list(engine: State<'_, Engine>, filter: Value) -> Result<Value, String> {
    engine.request("history.list", filter).await
}

#[tauri::command]
pub async fn history_get(
    engine: State<'_, Engine>,
    run_id: String,
    log_cursor: Option<u64>,
) -> Result<Value, String> {
    engine
        .request(
            "history.get",
            serde_json::json!({ "runId": run_id, "logCursor": log_cursor }),
        )
        .await
}

#[tauri::command]
pub async fn settings_get(engine: State<'_, Engine>) -> Result<Value, String> {
    engine.request("settings.get", serde_json::json!({})).await
}

#[tauri::command]
pub async fn settings_set(
    engine: State<'_, Engine>,
    log_retention: Option<Value>,
    hotkey: Option<Value>,
    close_to_tray: Option<bool>,
    first_close_tip_shown: Option<bool>,
    onboarding_completed: Option<bool>,
    updater: Option<Value>,
) -> Result<Value, String> {
    engine
        .request(
            "settings.set",
            serde_json::json!({
                "logRetention": log_retention, "hotkey":hotkey,
                "closeToTray":close_to_tray, "firstCloseTipShown":first_close_tip_shown,
                "onboardingCompleted":onboarding_completed, "updater":updater
            }),
        )
        .await
}

#[tauri::command]
pub async fn tray_run_last(app: tauri::AppHandle) -> Result<Value, String> {
    crate::tray::run_last(&app).await
}

#[tauri::command]
pub async fn script_run(
    engine: State<'_, Engine>,
    script_id: String,
    args: Value,
) -> Result<Value, String> {
    engine
        .request(
            "script.run",
            serde_json::json!({ "scriptId": script_id, "args": args, "trigger": "manual" }),
        )
        .await
}

#[tauri::command]
pub async fn script_cancel(engine: State<'_, Engine>, run_id: String) -> Result<Value, String> {
    engine
        .request("script.cancel", serde_json::json!({ "runId": run_id }))
        .await
}

#[tauri::command]
pub async fn bash_detect(engine: State<'_, Engine>) -> Result<Value, String> {
    engine.request("bash.detect", serde_json::json!({})).await
}

#[tauri::command]
pub async fn bash_set_path(
    engine: State<'_, Engine>,
    path: Option<String>,
) -> Result<Value, String> {
    engine
        .request("bash.setPath", serde_json::json!({ "path": path }))
        .await
}

#[tauri::command]
pub fn engine_status(engine: State<'_, Engine>) -> Status {
    engine.status()
}

#[tauri::command]
pub async fn engine_ping(engine: State<'_, Engine>) -> Result<Value, String> {
    engine.request("system.ping", serde_json::json!({})).await
}

#[tauri::command]
pub async fn engine_shutdown(engine: State<'_, Engine>) -> Result<Value, String> {
    engine
        .request("system.shutdown", serde_json::json!({}))
        .await
}

#[tauri::command]
pub async fn script_list(
    engine: State<'_, Engine>,
    after_id: Option<String>,
    limit: Option<u32>,
) -> Result<Value, String> {
    engine
        .request(
            "script.list",
            serde_json::json!({ "afterId": after_id, "limit": limit }),
        )
        .await
}

#[tauri::command]
pub async fn script_get(
    engine: State<'_, Engine>,
    script_id: String,
    offset: Option<usize>,
    revision: Option<String>,
) -> Result<Value, String> {
    engine
        .request(
            "script.get",
            serde_json::json!({ "scriptId": script_id, "offset": offset, "revision": revision }),
        )
        .await
}

#[tauri::command]
pub async fn script_import(
    engine: State<'_, Engine>,
    source_path: String,
    conflict_policy: String,
) -> Result<Value, String> {
    engine
        .request(
            "script.import",
            serde_json::json!({ "sourcePath": source_path, "conflictPolicy": conflict_policy }),
        )
        .await
}

#[tauri::command]
pub async fn script_update_meta(
    engine: State<'_, Engine>,
    script_id: String,
    meta: Value,
) -> Result<Value, String> {
    engine
        .request(
            "script.updateMeta",
            serde_json::json!({ "scriptId": script_id, "meta": meta }),
        )
        .await
}

#[tauri::command]
pub async fn script_update_content(
    engine: State<'_, Engine>,
    upload: Value,
) -> Result<Value, String> {
    engine.request("script.updateContent", upload).await
}

#[tauri::command]
pub async fn script_delete(engine: State<'_, Engine>, script_id: String) -> Result<Value, String> {
    engine
        .request(
            "script.delete",
            serde_json::json!({ "scriptId": script_id }),
        )
        .await
}
