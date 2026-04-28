-- Retire LLM-driven self-healing (skill fix) feature.
-- See commit message and .claude/plans/you-ai-distributed-cerf.md.
-- The feature was unable to verify generated patches semantically and
-- depended on LLM training cutoff covering the broken API — neither holds in
-- practice. Re-introduction requires execution test + rollback path + domain
-- knowledge augmentation, none of which exist today.
DROP TABLE IF EXISTS skill_fixes;

-- skill_schedule.fix_attempts had no remaining writer after the feature was
-- removed; drop the column so the schema reflects reality.
ALTER TABLE skill_schedule DROP COLUMN fix_attempts;
