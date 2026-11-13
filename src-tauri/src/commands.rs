use crate::sidecar::{Engine, Status};
use serde_json::Value;
use tauri::State;

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
