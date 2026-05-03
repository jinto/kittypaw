# JWKS Testkit

Reserved package for JWKS fixture keys, local JWKS servers, and token signing
helpers.

Portal owns OAuth, device credentials, discovery, and JWKS. Add shared fixtures
here when Portal issuer tests, Chat verifier tests, or local E2E flows need
stable signed tokens without using production credentials.
