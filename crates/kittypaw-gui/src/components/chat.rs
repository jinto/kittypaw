use dioxus::prelude::*;

#[component]
pub fn ChatPanel() -> Element {
    let mut messages = use_signal::<Vec<(String, String)>>(Vec::new);
    let mut input_text = use_signal(String::new);

    rsx! {
        div { style: "flex: 1; display: flex; flex-direction: column; overflow: hidden;",

            // Messages area
            div { style: "flex: 1; overflow-y: auto; padding: 20px 24px;",
                if messages.read().is_empty() {
                    div { style: "display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100%; text-align: center;",
                        h1 { style: "font-size: 24px; font-weight: 600; color: #1e293b; margin: 0 0 10px;", "How can I help you?" }
                        p { style: "font-size: 15px; color: #64748b;", "I'm KittyPaw, your AI agent. I can run code, automate tasks, and answer questions." }
                    }
                } else {
                    for (i, (role, content)) in messages.read().iter().enumerate() {
                        ChatMessage { key: "{i}", role: role.clone(), content: content.clone() }
                    }
                }
            }

            // Input area
            div { style: "padding: 12px 16px; border-top: 1px solid #e2e8f0;",
                div { style: "display: flex; gap: 8px;",
                    input {
                        style: "flex: 1; padding: 10px 14px; border: 1px solid #d1d5db; border-radius: 10px; font-size: 14px; outline: none;",
                        placeholder: "Message KittyPaw...",
                        value: "{input_text}",
                        oninput: move |e| input_text.set(e.value()),
                        onkeypress: move |e| {
                            if e.key() == Key::Enter && !input_text.read().is_empty() {
                                let msg = input_text.read().clone();
                                messages.write().push(("user".into(), msg));
                                input_text.set(String::new());
                                messages.write().push(("assistant".into(), "(AI response will appear here)".into()));
                            }
                        },
                    }
                    button {
                        style: "padding: 10px 16px; background: #2563eb; color: #fff; border: none; border-radius: 10px; cursor: pointer; font-size: 14px;",
                        onclick: move |_| {
                            if !input_text.read().is_empty() {
                                let msg = input_text.read().clone();
                                messages.write().push(("user".into(), msg));
                                input_text.set(String::new());
                                messages.write().push(("assistant".into(), "(AI response will appear here)".into()));
                            }
                        },
                        "Send"
                    }
                }
            }
        }
    }
}

#[component]
fn ChatMessage(role: String, content: String) -> Element {
    let is_user = role == "user";
    let bg = if is_user { "#f1f5f9" } else { "#fff" };
    let align = if is_user { "flex-end" } else { "flex-start" };
    let label = if is_user { "You" } else { "KittyPaw" };
    let label_color = if is_user { "#64748b" } else { "#2563eb" };

    rsx! {
        div { style: "display: flex; flex-direction: column; align-items: {align}; margin-bottom: 16px;",
            span { style: "font-size: 11px; font-weight: 600; color: {label_color}; margin-bottom: 4px;", "{label}" }
            div { style: "max-width: 80%; padding: 10px 14px; background: {bg}; border-radius: 12px; font-size: 14px; color: #1e293b; line-height: 1.5; white-space: pre-wrap;",
                "{content}"
            }
        }
    }
}
