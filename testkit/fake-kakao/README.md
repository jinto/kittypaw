# Fake Kakao Testkit

Reserved package for reusable fake Kakao helpers.

The current Kakao tests cover relay and channel pieces separately. End-to-end
Kakao tests should add shared fake webhook payloads and fake callback servers
here, then wire them through `apps/kakao` and the Kittypaw Kakao channel.
