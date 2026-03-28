use dioxus::prelude::*;

#[component]
pub fn Sidebar(on_tab_change: EventHandler<String>, on_open_settings: EventHandler) -> Element {
    rsx! {
        div { class: "sidebar",
            style: "width: 220px; background: #1e293b; color: #fff; display: flex; flex-direction: column; padding: 16px;",

            // Logo
            div { style: "display: flex; align-items: center; gap: 8px; margin-bottom: 24px;",
                span { style: "font-size: 16px; font-weight: 600;", "KittyPaw" }
            }

            // Nav buttons
            div { style: "display: flex; flex-direction: column; gap: 4px; flex: 1;",
                SidebarButton { label: "New Chat", on_click: move |_| on_tab_change.call("chat".into()) }
                SidebarButton { label: "Skills", on_click: move |_| on_tab_change.call("skills".into()) }
            }

            // Settings at bottom
            button {
                style: "display: flex; align-items: center; gap: 8px; background: none; border: none; color: #94a3b8; padding: 8px; cursor: pointer; font-size: 13px;",
                onclick: move |_| on_open_settings.call(()),
                "Settings"
            }
        }
    }
}

#[component]
fn SidebarButton(label: &'static str, on_click: EventHandler) -> Element {
    rsx! {
        button {
            style: "display: flex; align-items: center; gap: 8px; background: rgba(255,255,255,0.05); border: none; color: #e2e8f0; padding: 8px 12px; border-radius: 8px; cursor: pointer; font-size: 13px; text-align: left;",
            onclick: move |_| on_click.call(()),
            "{label}"
        }
    }
}
