use std::io::Read;

use clap::{Parser, Subcommand};
use oochy_llm::provider::LlmProvider;
use tracing_subscriber::EnvFilter;

mod agent_loop;
mod schedule;
mod skill_executor;
mod store;
mod teach_loop;

#[derive(Parser)]
#[command(name = "oochy", version)]
struct Cli {
    #[command(subcommand)]
    command: Option<Commands>,
}

#[derive(Subcommand)]
enum Commands {
    /// Start all configured channels and run the event loop
    Serve {
        /// Address to bind the WebSocket server (default: 0.0.0.0:3000)
        #[arg(long, default_value = "0.0.0.0:3000")]
        bind: String,
    },
    /// Config management commands
    Config {
        #[command(subcommand)]
        command: ConfigCommands,
    },
    /// Agent management commands
    Agent {
        #[command(subcommand)]
        command: AgentCommands,
    },
    /// Teach the bot a new skill from a natural language description
    Teach {
        /// Description of the skill to teach
        description: Vec<String>,
    },
    /// Skill management commands
    Skills {
        #[command(subcommand)]
        command: SkillsCommands,
    },
    /// Run a taught skill
    Run {
        /// Name of the skill to run
        name: String,
        /// Dry-run mode: execute in sandbox with mock data, no real side effects
        #[arg(long)]
        dry_run: bool,
    },
}

#[derive(Subcommand)]
enum SkillsCommands {
    /// List all taught skills
    List,
    /// Disable a skill (stops it from triggering)
    Disable {
        /// Name of the skill to disable
        name: String,
    },
    /// Delete a skill permanently
    Delete {
        /// Name of the skill to delete
        name: String,
    },
    /// Explain a skill using LLM
    Explain {
        /// Name of the skill to explain
        name: String,
    },
}

#[derive(Subcommand)]
enum ConfigCommands {
    /// Load and validate oochy.toml, print summary
    Check,
}

#[derive(Subcommand)]
enum AgentCommands {
    /// List configured agents with their skills
    List,
}

#[tokio::main]
async fn main() {
    if std::env::var("OOCHY_LOG_FORMAT").as_deref() == Ok("json") {
        tracing_subscriber::fmt()
            .json()
            .with_env_filter(EnvFilter::from_default_env())
            .init();
    } else {
        tracing_subscriber::fmt()
            .with_env_filter(EnvFilter::from_default_env())
            .init();
    }

    let cli = Cli::parse();

    match cli.command {
        Some(Commands::Serve { bind }) => {
            run_serve(&bind).await;
        }
        Some(Commands::Config { command: ConfigCommands::Check }) => {
            run_config_check();
        }
        Some(Commands::Agent { command: AgentCommands::List }) => {
            run_agent_list();
        }
        Some(Commands::Teach { description }) => {
            let desc = description.join(" ");
            if desc.trim().is_empty() {
                eprintln!("Usage: oochy teach <description>");
                eprintln!("Example: oochy teach send me a daily joke every morning");
                std::process::exit(1);
            }
            run_teach_cli(&desc).await;
        }
        Some(Commands::Skills { command }) => {
            match command {
                SkillsCommands::List => run_skills_list(),
                SkillsCommands::Disable { name } => run_skills_disable(&name),
                SkillsCommands::Delete { name } => run_skills_delete(&name),
                SkillsCommands::Explain { name } => run_skills_explain(&name).await,
            }
        }
        Some(Commands::Run { name, dry_run }) => {
            run_skill_cli(&name, dry_run).await;
        }
        None => {
            run_stdin().await;
        }
    }
}

async fn run_serve(bind_addr: &str) {
    use oochy_channels::websocket::ServeWebSocketChannel;
    use oochy_core::types::EventType;

    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.llm.api_key.is_empty() {
        eprintln!("Error: OOCHY_API_KEY not set. Export your Claude API key:");
        eprintln!("  export OOCHY_API_KEY=sk-ant-...");
        std::process::exit(1);
    }

    let provider = oochy_llm::claude::ClaudeProvider::new(
        config.llm.api_key.clone(),
        config.llm.model.clone(),
        config.llm.max_tokens,
    );

    let sandbox = oochy_sandbox::sandbox::Sandbox::new(
        config.sandbox.timeout_secs,
        config.sandbox.memory_limit_mb,
    );

    let db_path = std::env::var("OOCHY_DB_PATH").unwrap_or_else(|_| "oochy.db".into());
    let store = store::Store::open(&db_path).unwrap_or_else(|e| {
        eprintln!("Database error: {e}");
        std::process::exit(1);
    });

    // Bounded mpsc channel for all incoming events
    let (event_tx, mut event_rx) = tokio::sync::mpsc::channel::<oochy_core::types::Event>(256);

    // Start WebSocket channel
    let ws_channel = ServeWebSocketChannel::new(bind_addr);
    let _ws_handle = ws_channel
        .spawn(event_tx.clone())
        .await
        .unwrap_or_else(|e| {
            eprintln!("Failed to start WebSocket channel: {e}");
            std::process::exit(1);
        });

    eprintln!("oochy serve started. WebSocket at ws://{}/ws/chat", bind_addr);
    eprintln!("Press Ctrl+C to stop.");

    // Spawn schedule evaluator
    let schedule_config = config.clone();
    let schedule_sandbox = oochy_sandbox::sandbox::Sandbox::new(
        config.sandbox.timeout_secs,
        config.sandbox.memory_limit_mb,
    );
    let db_path_sched = db_path.clone();
    tokio::spawn(async move {
        schedule::run_schedule_loop(&schedule_config, &schedule_sandbox, &db_path_sched).await;
    });

    // Graceful shutdown signal
    let (shutdown_tx, mut shutdown_rx) = tokio::sync::watch::channel(false);
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        tracing::info!("Shutting down...");
        let _ = shutdown_tx.send(true);
    });

    // Event processing loop
    loop {
        tokio::select! {
            _ = shutdown_rx.changed() => {
                tracing::info!("Shutdown signal received, exiting event loop.");
                break;
            }
            maybe_event = event_rx.recv() => {
                let event = match maybe_event {
                    Some(e) => e,
                    None => break,
                };
                // Capture session_id before moving event
                let session_id = match event.event_type {
                    EventType::WebChat => event
                        .payload
                        .get("session_id")
                        .and_then(|v| v.as_str())
                        .unwrap_or("default")
                        .to_string(),
                    EventType::Telegram => event
                        .payload
                        .get("chat_id")
                        .and_then(|v| v.as_str())
                        .unwrap_or("default")
                        .to_string(),
                };
                let event_type = event.event_type.clone();

                // Check for /teach command on Telegram
                let is_teach = event.event_type == EventType::Telegram
                    && event
                        .payload
                        .get("text")
                        .and_then(|v| v.as_str())
                        .map(|t| t.starts_with("/teach"))
                        .unwrap_or(false);

                // Extract raw event text for skill matching
                let raw_event_text = event
                    .payload
                    .get("text")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string();

                if is_teach {
                    let teach_text = raw_event_text.strip_prefix("/teach").unwrap_or("").trim();
                    let chat_id_str = event
                        .payload
                        .get("chat_id")
                        .map(|v| {
                            v.as_str()
                                .map(|s| s.to_string())
                                .unwrap_or_else(|| v.to_string())
                        })
                        .unwrap_or_default();

                    if teach_text.is_empty() {
                        send_telegram_message(&config, &chat_id_str, "Usage: /teach <description>\n\nExample: /teach send me a daily joke").await;
                    } else {
                        send_telegram_message(&config, &chat_id_str, &format!("Generating skill for: {teach_text}...")).await;
                        match teach_loop::handle_teach(teach_text, &chat_id_str, &provider, &sandbox, &config).await {
                            Ok(ref result @ teach_loop::TeachResult::Generated { ref code, ref dry_run_output, ref skill_name, .. }) => {
                                match teach_loop::approve_skill(result) {
                                    Ok(()) => {
                                        let msg = format!(
                                            "Skill '{skill_name}' generated and saved!\n\nCode:\n{code}\n\nDry-run output: {dry_run_output}"
                                        );
                                        send_telegram_message(&config, &chat_id_str, &msg).await;
                                    }
                                    Err(e) => {
                                        send_telegram_message(&config, &chat_id_str, &format!("Failed to save skill: {e}")).await;
                                    }
                                }
                            }
                            Ok(teach_loop::TeachResult::Error(e)) => {
                                send_telegram_message(&config, &chat_id_str, &format!("Teach failed: {e}")).await;
                            }
                            Err(e) => {
                                send_telegram_message(&config, &chat_id_str, &format!("Error: {e}")).await;
                            }
                        }
                    }
                    continue;
                }

                // Check taught skills before falling through to agent loop
                let skills = oochy_core::skill::load_all_skills();
                let matched_skill = match skills {
                    Ok(ref skill_list) => skill_list.iter().find(|(skill, _js)| {
                        skill.enabled && oochy_core::skill::match_trigger(skill, &raw_event_text)
                    }),
                    Err(ref e) => {
                        tracing::warn!("Failed to load skills: {e}");
                        None
                    }
                };

                if let Some((skill, js_code)) = matched_skill {
                    let wrapped_code = format!("const ctx = JSON.parse(__context__);\n{}", js_code);
                    let context = serde_json::json!({
                        "event_type": format!("{:?}", event_type).to_lowercase(),
                        "event_text": raw_event_text,
                        "chat_id": session_id,
                    });

                    match sandbox.execute(&wrapped_code, context).await {
                        Ok(exec_result) => {
                            if !exec_result.skill_calls.is_empty() {
                                let _ = crate::skill_executor::execute_skill_calls(&exec_result.skill_calls, &config, Some(&skill.name)).await;
                            }
                            let output = if exec_result.output.is_empty() {
                                "(no output)".to_string()
                            } else {
                                exec_result.output.clone()
                            };
                            match event_type {
                                EventType::WebChat => {
                                    if let Err(e) = ws_channel.send_to_session(&session_id, &output).await {
                                        tracing::warn!("Failed to send WebSocket response: {e}");
                                    }
                                }
                                EventType::Telegram => {
                                    send_telegram_message(&config, &session_id, &output).await;
                                }
                            }
                        }
                        Err(e) => {
                            tracing::error!("Skill execution error for session {session_id}: {e}");
                            match event_type {
                                EventType::WebChat => {
                                    let _ = ws_channel.send_to_session(&session_id, &format!("Error: {e}")).await;
                                }
                                EventType::Telegram => {
                                    send_telegram_message(&config, &session_id, &format!("Skill error: {e}")).await;
                                }
                            }
                        }
                    }
                    continue;
                }

                // No skill matched — check freeform fallback
                if !config.freeform_fallback {
                    let msg = "No matching skill found. Use /teach to create one.";
                    match event_type {
                        EventType::WebChat => {
                            let _ = ws_channel.send_to_session(&session_id, msg).await;
                        }
                        EventType::Telegram => {
                            send_telegram_message(&config, &session_id, msg).await;
                        }
                    }
                    continue;
                }

                match agent_loop::run_agent_loop(event, &provider, &sandbox, &store, &config).await {
                    Ok(output) => {
                        // Route response back to originating channel
                        match event_type {
                            EventType::WebChat => {
                                if let Err(e) = ws_channel.send_to_session(&session_id, &output).await {
                                    tracing::warn!("Failed to send WebSocket response: {e}");
                                }
                            }
                            EventType::Telegram => {
                                // Other channels handle their own responses via skill calls
                                tracing::info!("Agent response for {session_id}: {output}");
                            }
                        }
                    }
                    Err(e) => {
                        tracing::error!("Agent loop error for session {session_id}: {e}");
                        let _ = ws_channel
                            .send_to_session(&session_id, &format!("Error: {e}"))
                            .await;
                    }
                }
            }
        }
    }
}

async fn send_telegram_message(config: &oochy_core::config::Config, chat_id: &str, text: &str) {
    let bot_token = match std::env::var("OOCHY_TELEGRAM_TOKEN") {
        Ok(t) => t,
        Err(_) => {
            // Try channel config
            config
                .channels
                .iter()
                .find(|c| c.channel_type == "telegram")
                .map(|c| c.token.clone())
                .unwrap_or_default()
        }
    };
    if bot_token.is_empty() {
        tracing::warn!("Cannot send Telegram message: no bot token configured");
        return;
    }
    let url = format!("https://api.telegram.org/bot{bot_token}/sendMessage");
    let client = reqwest::Client::new();
    let res = client
        .post(&url)
        .json(&serde_json::json!({
            "chat_id": chat_id,
            "text": text,
        }))
        .send()
        .await;
    if let Err(e) = res {
        tracing::warn!("Failed to send Telegram message: {e}");
    }
}

fn run_config_check() {
    match oochy_core::config::Config::load() {
        Ok(config) => {
            println!("Config OK");
            println!("  LLM provider : {}", config.llm.provider);
            println!("  LLM model    : {}", config.llm.model);
            println!("  Max tokens   : {}", config.llm.max_tokens);
            println!(
                "  API key      : {}",
                if config.llm.api_key.is_empty() { "NOT SET" } else { "set" }
            );
            println!("  Sandbox timeout : {}s", config.sandbox.timeout_secs);
            println!("  Sandbox memory  : {}MB", config.sandbox.memory_limit_mb);
            println!("  Channels : {}", config.channels.len());
            for ch in &config.channels {
                let addr = ch.bind_addr.as_deref().unwrap_or("-");
                println!("    - {} (bind={}, token={})", ch.channel_type, addr,
                    if ch.token.is_empty() { "not set" } else { "set" });
            }
            println!("  Agents   : {}", config.agents.len());
            for agent in &config.agents {
                println!("    - {} ({})", agent.name, agent.id);
            }
            if config.llm.api_key.is_empty() {
                eprintln!("Warning: API key not set. Set OOCHY_API_KEY or llm.api_key in oochy.toml");
                std::process::exit(1);
            }
        }
        Err(e) => {
            eprintln!("Config error: {e}");
            std::process::exit(1);
        }
    }
}

fn run_agent_list() {
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.agents.is_empty() {
        println!("No agents configured. Add [[agents]] sections to oochy.toml");
        return;
    }

    for agent in &config.agents {
        println!("Agent: {} (id={})", agent.name, agent.id);
        println!("  System prompt: {}...", &agent.system_prompt.chars().take(60).collect::<String>());
        println!("  Channels: {}", if agent.channels.is_empty() { "none".to_string() } else { agent.channels.join(", ") });
        if agent.allowed_skills.is_empty() {
            println!("  Skills: none");
        } else {
            println!("  Skills:");
            for skill in &agent.allowed_skills {
                let methods = if skill.methods.is_empty() {
                    "all".to_string()
                } else {
                    skill.methods.join(", ")
                };
                println!("    - {} [{}] (rate: {}/min)", skill.skill, methods, skill.rate_limit_per_minute);
            }
        }
    }
}

fn run_skills_list() {
    let skills = oochy_core::skill::load_all_skills();
    match skills {
        Err(e) => {
            eprintln!("Error loading skills: {e}");
            std::process::exit(1);
        }
        Ok(ref list) if list.is_empty() => {
            println!("No skills found. Use 'oochy teach' to create one.");
        }
        Ok(list) => {
            println!("Skills:");
            println!("  {:<16} | {:<7} | {:<8} | {:<18} | enabled", "name", "version", "trigger", "schedule");
            for (skill, _) in &list {
                let schedule = if skill.trigger.trigger_type == "schedule" {
                    skill.trigger.natural.as_deref()
                        .or(skill.trigger.cron.as_deref())
                        .unwrap_or("—")
                        .to_string()
                } else {
                    "—".to_string()
                };
                let enabled = if skill.enabled { "yes" } else { "no" };
                println!(
                    "  {:<16} | {:<7} | {:<8} | {:<18} | {}",
                    skill.name, skill.version, skill.trigger.trigger_type, schedule, enabled
                );
            }
        }
    }
}

fn run_skills_disable(name: &str) {
    match oochy_core::skill::disable_skill(name) {
        Ok(()) => println!("Skill '{name}' disabled."),
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}

fn run_skills_delete(name: &str) {
    match oochy_core::skill::delete_skill(name) {
        Ok(()) => println!("Skill '{name}' deleted."),
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_skills_explain(name: &str) {
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.llm.api_key.is_empty() {
        eprintln!("Error: OOCHY_API_KEY not set.");
        std::process::exit(1);
    }

    match oochy_core::skill::load_skill(name) {
        Ok(Some((skill, js_code))) => {
            let provider = oochy_llm::claude::ClaudeProvider::new(
                config.llm.api_key.clone(),
                config.llm.model.clone(),
                config.llm.max_tokens,
            );

            let prompt = format!(
                "Explain this JavaScript skill in plain English. What does it do, what permissions does it need, and when does it run?\n\nSkill name: {}\nTrigger: {} {}\nPermissions: {}\n\nCode:\n{}",
                skill.name,
                skill.trigger.trigger_type,
                skill.trigger.cron.as_deref().or(skill.trigger.keyword.as_deref()).unwrap_or(""),
                skill.permissions.primitives.join(", "),
                js_code
            );

            let messages = vec![oochy_core::types::LlmMessage {
                role: oochy_core::types::Role::User,
                content: prompt,
            }];

            match provider.generate(&messages).await {
                Ok(explanation) => println!("{explanation}"),
                Err(e) => eprintln!("Failed to generate explanation: {e}"),
            }
        }
        Ok(None) => {
            eprintln!("Skill '{name}' not found.");
            std::process::exit(1);
        }
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_skill_cli(name: &str, dry_run: bool) {
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    match oochy_core::skill::load_skill(name) {
        Ok(Some((skill, js_code))) => {
            let sandbox = oochy_sandbox::sandbox::Sandbox::new(
                config.sandbox.timeout_secs,
                config.sandbox.memory_limit_mb,
            );

            let context = serde_json::json!({
                "event_type": "cli",
                "event_text": "",
                "chat_id": "",
                "skill_name": skill.name,
            });
            let wrapped = format!("const ctx = JSON.parse(__context__);\n{js_code}");

            match sandbox.execute(&wrapped, context).await {
                Ok(result) if result.success => {
                    println!("Output: {}", result.output);
                    if !result.skill_calls.is_empty() {
                        if dry_run {
                            println!("\n[dry-run] Skill calls that would execute:");
                            for call in &result.skill_calls {
                                println!("  {}.{}({:?})", call.skill_name, call.method, call.args);
                            }
                        } else {
                            match skill_executor::execute_skill_calls(&result.skill_calls, &config, Some(&skill.name)).await {
                                Ok(results) => {
                                    for r in &results {
                                        if r.success {
                                            println!("  {}.{}: OK", r.skill_name, r.method);
                                        } else {
                                            eprintln!("  {}.{}: FAILED {:?}", r.skill_name, r.method, r.error);
                                        }
                                    }
                                }
                                Err(e) => eprintln!("Skill execution error: {e}"),
                            }
                        }
                    }
                }
                Ok(result) => {
                    eprintln!("Skill failed: {:?}", result.error);
                    std::process::exit(1);
                }
                Err(e) => {
                    eprintln!("Execution error: {e}");
                    std::process::exit(1);
                }
            }
        }
        Ok(None) => {
            eprintln!("Skill '{name}' not found.");
            std::process::exit(1);
        }
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_teach_cli(description: &str) {
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.llm.api_key.is_empty() {
        eprintln!("Error: OOCHY_API_KEY not set.");
        std::process::exit(1);
    }

    let provider = oochy_llm::claude::ClaudeProvider::new(
        config.llm.api_key.clone(),
        config.llm.model.clone(),
        config.llm.max_tokens,
    );

    let sandbox = oochy_sandbox::sandbox::Sandbox::new(
        config.sandbox.timeout_secs,
        config.sandbox.memory_limit_mb,
    );

    println!("Generating skill for: {description}...\n");

    loop {
        match teach_loop::handle_teach(description, "cli", &provider, &sandbox, &config).await {
            Ok(
                ref result @ teach_loop::TeachResult::Generated {
                    ref code,
                    ref dry_run_output,
                    ref skill_name,
                    ref description,
                    ref permissions,
                    ..
                },
            ) => {
                println!("=== Generated Skill: {skill_name} ===\n");
                println!("Description: {description}");
                println!("Permissions: {}", permissions.join(", "));
                println!("\nCode:\n{code}\n");
                println!("Dry-run output: {dry_run_output}\n");

                // Interactive prompt
                eprint!("[a]pprove / [r]eject / re[g]enerate? ");
                let mut input = String::new();
                std::io::stdin().read_line(&mut input).unwrap_or_default();
                let choice = input.trim().to_lowercase();

                match choice.as_str() {
                    "a" | "approve" | "y" | "yes" => match teach_loop::approve_skill(result) {
                        Ok(()) => {
                            println!("Skill '{skill_name}' saved to .oochy/skills/");
                            return;
                        }
                        Err(e) => {
                            eprintln!("Failed to save: {e}");
                            std::process::exit(1);
                        }
                    },
                    "r" | "reject" | "n" | "no" => {
                        println!("Skill rejected.");
                        return;
                    }
                    "g" | "regenerate" => {
                        println!("\nRegenerating...\n");
                        continue;
                    }
                    _ => {
                        println!("Unknown choice. Skill rejected.");
                        return;
                    }
                }
            }
            Ok(teach_loop::TeachResult::Error(e)) => {
                eprintln!("Teach failed: {e}");
                std::process::exit(1);
            }
            Err(e) => {
                eprintln!("Error: {e}");
                std::process::exit(1);
            }
        }
    }
}

async fn run_stdin() {
    // Read event from stdin
    let mut input = String::new();
    if atty::is(atty::Stream::Stdin) {
        eprintln!("oochy v{}", env!("CARGO_PKG_VERSION"));
        eprintln!("Usage: echo '{{\"type\":\"web_chat\",\"payload\":{{\"text\":\"hello\"}}}}' | oochy");
        eprintln!("       oochy serve [--bind 0.0.0.0:3000]");
        std::process::exit(0);
    }

    std::io::stdin().read_to_string(&mut input).expect("failed to read stdin");
    let input = input.trim();
    if input.is_empty() {
        eprintln!("Error: empty input");
        std::process::exit(1);
    }

    // Parse event
    let event: oochy_core::types::Event = match serde_json::from_str(input) {
        Ok(e) => e,
        Err(e) => {
            eprintln!("Error parsing event JSON: {e}");
            eprintln!("Expected: {{\"type\":\"web_chat\",\"payload\":{{\"text\":\"hello\"}}}}");
            std::process::exit(1);
        }
    };

    // Load config
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.llm.api_key.is_empty() {
        eprintln!("Error: OOCHY_API_KEY not set. Export your Claude API key:");
        eprintln!("  export OOCHY_API_KEY=sk-ant-...");
        std::process::exit(1);
    }

    // Initialize components
    let provider = oochy_llm::claude::ClaudeProvider::new(
        config.llm.api_key.clone(),
        config.llm.model.clone(),
        config.llm.max_tokens,
    );

    let sandbox = oochy_sandbox::sandbox::Sandbox::new(
        config.sandbox.timeout_secs,
        config.sandbox.memory_limit_mb,
    );

    let db_path = std::env::var("OOCHY_DB_PATH").unwrap_or_else(|_| "oochy.db".into());
    let store = store::Store::open(&db_path).unwrap_or_else(|e| {
        eprintln!("Database error: {e}");
        std::process::exit(1);
    });

    // Run agent loop
    match agent_loop::run_agent_loop(event, &provider, &sandbox, &store, &config).await {
        Ok(output) => {
            println!("{output}");
        }
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}
