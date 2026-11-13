use serde::Deserialize;
use serde_json::{json, Value};
use std::io::{self, BufRead};
use uuid::Uuid;

const MAX_LINE: usize = 1 << 20;

#[derive(Debug, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub(super) enum Incoming {
    Evt {
        v: u32,
        #[serde(rename = "requestId")]
        request_id: Option<Uuid>,
        event: String,
        payload: Value,
    },
    Res {
        v: u32,
        #[serde(rename = "requestId")]
        request_id: String,
        ok: bool,
        payload: Option<Value>,
        error: Option<Failure>,
    },
}

#[derive(Debug, Deserialize)]
pub(super) struct Failure {
    pub code: String,
    pub message: String,
    pub details: Value,
}

pub(super) fn encode_request(id: Uuid, action: &str, payload: Value) -> Result<Vec<u8>, String> {
    let mut line = serde_json::to_vec(&json!({
        "v": 1, "type": "req", "requestId": id.to_string(), "action": action, "payload": payload
    }))
    .map_err(|error| error.to_string())?;
    if line.len() > MAX_LINE {
        return Err("SIZE_LIMIT：请求超过 1 MiB …[truncated]".into());
    }
    line.push(b'\n');
    Ok(line)
}

#[derive(Debug)]
pub(super) struct ProtocolError {
    pub message: &'static str,
    pub truncated: bool,
}

pub(super) fn read_message(
    reader: &mut impl BufRead,
) -> io::Result<Option<Result<Incoming, ProtocolError>>> {
    let Some(line) = read_line(reader)? else {
        return Ok(None);
    };
    Ok(Some(line.and_then(|line| {
        serde_json::from_slice(&line).map_err(|_| ProtocolError {
            message: "Sidecar stdout contains an invalid JSONL envelope",
            truncated: false,
        })
    })))
}

fn read_line(reader: &mut impl BufRead) -> io::Result<Option<Result<Vec<u8>, ProtocolError>>> {
    let mut line = Vec::new();
    let mut overflow = false;
    loop {
        let part = reader.fill_buf()?;
        if part.is_empty() {
            return Ok(if overflow {
                Some(Err(overlong_line()))
            } else if line.is_empty() {
                None
            } else {
                Some(Err(ProtocolError {
                    message: "Sidecar stdout line is missing its newline",
                    truncated: false,
                }))
            });
        }
        let newline = part.iter().position(|&byte| byte == b'\n');
        let length = newline.unwrap_or(part.len());
        if !overflow {
            if length > MAX_LINE - line.len() {
                line = Vec::new();
                overflow = true;
            } else {
                line.extend_from_slice(&part[..length]);
            }
        }
        reader.consume(length + usize::from(newline.is_some()));
        if newline.is_some() {
            return Ok(Some(if overflow {
                Err(overlong_line())
            } else {
                Ok(line)
            }));
        }
    }
}

fn overlong_line() -> ProtocolError {
    ProtocolError {
        message: "Sidecar stdout line exceeds 1 MiB …[truncated]",
        truncated: true,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{BufReader, Cursor};

    #[test]
    fn malformed_and_overlong_stdout_preserve_the_next_message() {
        let mut input = b"debug output\n".to_vec();
        input.extend(std::iter::repeat_n(b'x', MAX_LINE + 1));
        input.extend_from_slice(
            b"\n{\"v\":1,\"type\":\"evt\",\"event\":\"system.hello\",\"payload\":{}}\n",
        );
        let mut reader = BufReader::new(Cursor::new(input));
        assert!(
            !read_message(&mut reader)
                .unwrap()
                .unwrap()
                .unwrap_err()
                .truncated
        );
        assert!(
            read_message(&mut reader)
                .unwrap()
                .unwrap()
                .unwrap_err()
                .truncated
        );
        assert!(matches!(
            read_message(&mut reader).unwrap(),
            Some(Ok(Incoming::Evt { .. }))
        ));
        assert!(read_message(&mut reader).unwrap().is_none());
    }
}
