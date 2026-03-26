use rquickjs::{async_with, AsyncContext, AsyncRuntime, Function, Object, Promise, prelude::Async};

#[tokio::main]
async fn main() {
    println!("=== Spike 0.1: rquickjs Async Skill Roundtrip ===\n");

    let rt = AsyncRuntime::new().expect("create AsyncRuntime");
    let ctx = AsyncContext::full(&rt).await.expect("create AsyncContext");

    // Do everything inside one async_with! block so Promise lifetime is valid
    let success = async_with!(ctx => |ctx| {
        let globals = ctx.globals();

        // Create Telegram namespace object
        let telegram = Object::new(ctx.clone()).expect("create Telegram object");

        // Register async host function: Telegram.sendMessage(chatId, text) -> Promise<string>
        let send_fn = Function::new(
            ctx.clone(),
            Async(|chat_id: String, text: String| async move {
                println!("[Rust] Telegram.sendMessage called: chatId={chat_id}, text={text}");

                let client = reqwest::Client::new();
                let payload = serde_json::json!({
                    "chatId": chat_id,
                    "text": text,
                });

                let response = client
                    .post("https://httpbin.org/post")
                    .json(&payload)
                    .send()
                    .await
                    .unwrap();

                let status = response.status().is_success();
                let body: serde_json::Value = response.json().await.unwrap();

                println!("[Rust] HTTP response received from httpbin.org (ok={status})");

                let result = serde_json::json!({
                    "ok": status,
                    "data": { "url": body["url"] },
                });
                result.to_string()
            }),
        )
        .expect("create sendMessage function")
        .with_name("sendMessage")
        .expect("set function name");

        telegram.set("sendMessage", send_fn).expect("set sendMessage on Telegram");
        globals.set("Telegram", telegram).expect("set Telegram global");

        // Evaluate async JS — get the Promise
        let promise: Promise = ctx
            .eval(
                r#"
                    (async function() {
                        const rawResult = await Telegram.sendMessage("123", "hello");
                        const result = JSON.parse(rawResult);
                        return result.ok ? "success" : "failure";
                    })()
                "#,
            )
            .expect("eval JS async IIFE");

        // Await the promise directly inside async_with! — this drives both
        // the QuickJS event loop and the Tokio runtime cooperatively
        let result: Result<String, _> = promise.into_future().await;

        match result {
            Ok(val) => {
                println!("[Rust] Promise resolved with: {val}");
                val == "success"
            }
            Err(e) => {
                println!("[Rust] Promise rejected: {e:?}");
                false
            }
        }
    })
    .await;

    if success {
        println!("\n=== Spike 0.1 PASSED ===");
        println!("Proven:");
        println!("  1. JS `await Telegram.sendMessage(...)` works in rquickjs");
        println!("  2. Host Rust async function executes real HTTP call via Tokio");
        println!("  3. Promise resolves with HTTP response data");
        println!("  4. No deadlock or thread starvation");
        println!("  5. Works with #[tokio::main] (multi-threaded runtime)");
    } else {
        println!("\n=== Spike 0.1 FAILED ===");
    }
}
