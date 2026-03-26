use std::collections::HashMap;
use std::time::Instant;

use crate::config::AgentConfig;
use crate::error::{OochyError, Result};
use crate::types::SkillCall;

pub struct CapabilityChecker {
    allowed_skills: HashMap<String, SkillPermissionEntry>,
}

struct SkillPermissionEntry {
    methods: Vec<String>,
    rate_limit_per_minute: u32,
    call_timestamps: Vec<Instant>,
}

impl CapabilityChecker {
    pub fn from_agent_config(config: &AgentConfig) -> Self {
        let mut allowed_skills = HashMap::new();
        for perm in &config.allowed_skills {
            allowed_skills.insert(
                perm.skill.clone(),
                SkillPermissionEntry {
                    methods: perm.methods.clone(),
                    rate_limit_per_minute: perm.rate_limit_per_minute,
                    call_timestamps: Vec::new(),
                },
            );
        }
        Self { allowed_skills }
    }

    pub fn check(&mut self, call: &SkillCall) -> Result<()> {
        let entry = self
            .allowed_skills
            .get_mut(&call.skill_name)
            .ok_or_else(|| {
                OochyError::CapabilityDenied(format!(
                    "Skill '{}' is not allowed for this agent",
                    call.skill_name
                ))
            })?;

        // Check method is allowed (empty = all methods allowed)
        if !entry.methods.is_empty() && !entry.methods.contains(&call.method) {
            return Err(OochyError::CapabilityDenied(format!(
                "Method '{}.{}' is not allowed",
                call.skill_name, call.method
            )));
        }

        // Token bucket rate limiting
        let now = Instant::now();
        let window = std::time::Duration::from_secs(60);
        entry.call_timestamps.retain(|t| now.duration_since(*t) < window);

        if entry.call_timestamps.len() >= entry.rate_limit_per_minute as usize {
            return Err(OochyError::RateLimitExceeded(format!(
                "Skill '{}' exceeded {} calls/minute",
                call.skill_name, entry.rate_limit_per_minute
            )));
        }

        entry.call_timestamps.push(now);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::SkillPermission;

    fn test_agent_config() -> AgentConfig {
        AgentConfig {
            id: "test".into(),
            name: "Test Agent".into(),
            system_prompt: String::new(),
            channels: vec![],
            allowed_skills: vec![SkillPermission {
                skill: "Telegram".into(),
                methods: vec!["sendMessage".into()],
                rate_limit_per_minute: 3,
            }],
        }
    }

    #[test]
    fn test_allowed_skill_passes() {
        let mut checker = CapabilityChecker::from_agent_config(&test_agent_config());
        let call = SkillCall {
            skill_name: "Telegram".into(),
            method: "sendMessage".into(),
            args: vec![],
        };
        assert!(checker.check(&call).is_ok());
    }

    #[test]
    fn test_denied_skill_rejected() {
        let mut checker = CapabilityChecker::from_agent_config(&test_agent_config());
        let call = SkillCall {
            skill_name: "Discord".into(),
            method: "sendMessage".into(),
            args: vec![],
        };
        assert!(checker.check(&call).is_err());
    }

    #[test]
    fn test_denied_method_rejected() {
        let mut checker = CapabilityChecker::from_agent_config(&test_agent_config());
        let call = SkillCall {
            skill_name: "Telegram".into(),
            method: "deleteMessage".into(),
            args: vec![],
        };
        assert!(checker.check(&call).is_err());
    }

    #[test]
    fn test_rate_limit() {
        let mut checker = CapabilityChecker::from_agent_config(&test_agent_config());
        let call = SkillCall {
            skill_name: "Telegram".into(),
            method: "sendMessage".into(),
            args: vec![],
        };
        assert!(checker.check(&call).is_ok());
        assert!(checker.check(&call).is_ok());
        assert!(checker.check(&call).is_ok());
        // 4th call should fail (limit is 3/min)
        assert!(checker.check(&call).is_err());
    }
}
