.PHONY: help list contracts-check smoke-local e2e-local

help:
	@echo "Targets:"
	@echo "  list             List skeleton files"
	@echo "  contracts-check  Validate JSON contract files with jq"
	@echo "  smoke-local      Run repeatable local cross-service smoke"
	@echo "  e2e-local        Run Docker-backed local auth/chat E2E"

list:
	@find . -maxdepth 5 -type f | sort

contracts-check:
	@find contracts -name '*.json' -print0 | xargs -0 -n1 jq empty

smoke-local:
	@scripts/smoke-local.sh

e2e-local:
	@scripts/e2e-local.sh
