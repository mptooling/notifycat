# Changelog

## [0.10.0](https://github.com/mptooling/notifycat/compare/v0.9.0...v0.10.0) (2026-06-04)


### 🚀 Features

* build multi-arch docker image (linux/amd64 + linux/arm64) in CI ([#63](https://github.com/mptooling/notifycat/issues/63)) ([4f2df81](https://github.com/mptooling/notifycat/commit/4f2df8107168bbe314f953cc420f8fe7fc956fdd))

## [0.9.0](https://github.com/mptooling/notifycat/compare/v0.8.0...v0.9.0) (2026-06-04)


### 🚀 Features

* react to general PR conversation comments (issue_comment event) ([#61](https://github.com/mptooling/notifycat/issues/61)) ([4ca980f](https://github.com/mptooling/notifycat/commit/4ca980fe964bf60671c1f132951b92aaf09bc785))

## [0.8.0](https://github.com/mptooling/notifycat/compare/v0.7.0...v0.8.0) (2026-06-03)


### 🚀 Features

* notifycat setup wizard and host dispatch wrapper ([#57](https://github.com/mptooling/notifycat/issues/57)) ([c8be8c8](https://github.com/mptooling/notifycat/commit/c8be8c817ab33f91c975a8c7ce82bcbd831cfd56))

## [0.7.0](https://github.com/mptooling/notifycat/compare/v0.6.1...v0.7.0) (2026-06-01)


### 🚀 Features

* friendly first-run errors for missing or invalid config ([#55](https://github.com/mptooling/notifycat/issues/55)) ([8a70c61](https://github.com/mptooling/notifycat/commit/8a70c61081f097b12b5dccef67b9edf546bd3486))

## [0.6.1](https://github.com/mptooling/notifycat/compare/v0.6.0...v0.6.1) (2026-06-01)


### 📝 Documentation

* docker compose install guide with HTTPS and troubleshooting ([#53](https://github.com/mptooling/notifycat/issues/53)) ([086d6e2](https://github.com/mptooling/notifycat/commit/086d6e26ff977626f7f335b1fe6e8b6deb8eb662))

## [0.6.0](https://github.com/mptooling/notifycat/compare/v0.5.0...v0.6.0) (2026-06-01)


### 🚀 Features

* docker-compose.yml with Caddy HTTPS reverse proxy ([#51](https://github.com/mptooling/notifycat/issues/51)) ([06cbbe9](https://github.com/mptooling/notifycat/commit/06cbbe91a6e560e55143621ca785bc9f0533b35c))

## [0.5.0](https://github.com/mptooling/notifycat/compare/v0.4.0...v0.5.0) (2026-06-01)


### 🚀 Features

* opt-in suppression of Slack reactions from bot reviewers (closes [#14](https://github.com/mptooling/notifycat/issues/14)) ([#29](https://github.com/mptooling/notifycat/issues/29)) ([f375bb2](https://github.com/mptooling/notifycat/commit/f375bb2a872d3163f1a97e4026f7b6cc15604384))

## [0.4.0](https://github.com/mptooling/notifycat/compare/v0.3.1...v0.4.0) (2026-05-20)

### ⚠ BREAKING CHANGES

* docker image with /app workdir and bundled doctor ([#38](https://github.com/mptooling/notifycat/issues/38))

### 🐛 Bug fixes

* docker image with /app workdir and bundled doctor ([#38](https://github.com/mptooling/notifycat/issues/38)) ([906d9b7](https://github.com/mptooling/notifycat/commit/906d9b731978b2d7dc3e9bedb3923af77fb9223e))

## [0.3.1](https://github.com/mptooling/notifycat/compare/v0.3.0...v0.3.1) (2026-05-19)

### 📝 Documentation

* refresh project overview ([#35](https://github.com/mptooling/notifycat/issues/35)) ([4f7cdce](https://github.com/mptooling/notifycat/commit/4f7cdce7d2984a4e999d8d54f37aa41fea373f98))

## [0.3.0](https://github.com/mptooling/notifycat/compare/v0.2.1...v0.3.0) (2026-05-19)

### 🚀 Features

* add notifycat-doctor preflight diagnostics (closes [#2](https://github.com/mptooling/notifycat/issues/2)) ([#27](https://github.com/mptooling/notifycat/issues/27)) ([1b53335](https://github.com/mptooling/notifycat/commit/1b53335016193a5702ae2cabd8b963dd3a544f6f))

## [0.2.1](https://github.com/mptooling/notifycat/compare/v0.2.0...v0.2.1) (2026-05-18)

### 🐛 Bug fixes

* align release-please manifest and CHANGELOG with v0.2.0 tag ([#25](https://github.com/mptooling/notifycat/issues/25)) ([ce9ca98](https://github.com/mptooling/notifycat/commit/ce9ca9867d76c4e3d4c989214f68ceb70f3bda00))

## 0.2.0 (2026-05-18)

### 🚀 Features

* **app:** composition root + githubhook HTTP handler ([7480310](https://github.com/mptooling/notifycat/commit/74803100568199a53e183787313fea9d0786e578))
* **config:** add redacted Secret type ([7bdea86](https://github.com/mptooling/notifycat/commit/7bdea8681282a268f1c7e4a68b269e2881658039))
* **config:** load runtime config from env with validated required vars ([b8ddb73](https://github.com/mptooling/notifycat/commit/b8ddb7391e5da0f354fa52c0e04996e55e9ff6d2))
* declarative mappings.yaml + per-entry lock (closes [#8](https://github.com/mptooling/notifycat/issues/8)) ([#9](https://github.com/mptooling/notifycat/issues/9)) ([1d8f92b](https://github.com/mptooling/notifycat/commit/1d8f92b7fb4a8c1c2cea7bbec6a4b884c3d3d406))
* default to `[@channel](https://github.com/channel)` when mappings.yaml omits `mentions:` (closes [#10](https://github.com/mptooling/notifycat/issues/10)) ([#12](https://github.com/mptooling/notifycat/issues/12)) ([365dafc](https://github.com/mptooling/notifycat/commit/365dafcd5e7a4bc604a938a413fc6c42bfc0e7db))
* **githubhook:** HMAC verifier, signature middleware, payload parser ([5d43916](https://github.com/mptooling/notifycat/commit/5d439165fb3e599c516a9c81be17af8b6497f46b))
* **mapping:** CLI to add/list/remove repo→channel mappings ([3a09999](https://github.com/mptooling/notifycat/commit/3a099997ff88810492328056df1553a38aa30e6a))
* open source documents and automation ([37d771f](https://github.com/mptooling/notifycat/commit/37d771f248934095920f32a490bf7c2d313010b8))
* **pullrequest:** Close, Draft, Approve, Commented, RequestChange handlers ([798976e](https://github.com/mptooling/notifycat/commit/798976eeac5cb3967e90070177b6155ecf33cb9e))
* **pullrequest:** Event, Dispatcher, EventHandler, and OpenHandler ([1231cab](https://github.com/mptooling/notifycat/commit/1231cab45668b54a6e65ca9f1101ff28d2fcbcee))
* scheduled cleanup of stale slack_messages rows (closes [#7](https://github.com/mptooling/notifycat/issues/7)) ([#22](https://github.com/mptooling/notifycat/issues/22)) ([de27ea5](https://github.com/mptooling/notifycat/commit/de27ea5d22fc4cd218c241d112f513a6d2e5ce75))
* **server:** cmd/notifycat-server with graceful shutdown ([13963dc](https://github.com/mptooling/notifycat/commit/13963dc9171e617eb5fe89de4ceb4ca842f50bdc))
* **slack:** web API client + message composer ([63fbbe2](https://github.com/mptooling/notifycat/commit/63fbbe21464c609e9b705e5b1a358232b0e21994))
* **store:** GORM repositories + goose migrations + migrate CLI ([fbe7315](https://github.com/mptooling/notifycat/commit/fbe7315fa110de44797d537088ab1539364958a3))
* structured logs for ignored webhook events (closes [#4](https://github.com/mptooling/notifycat/issues/4)) ([#16](https://github.com/mptooling/notifycat/issues/16)) ([da8e1e7](https://github.com/mptooling/notifycat/commit/da8e1e736a005c16395311c7c557ef40ec0ea717))
* validate mapping setup before runtime ([#6](https://github.com/mptooling/notifycat/issues/6)) ([462b2c4](https://github.com/mptooling/notifycat/commit/462b2c420c6caf902b8d85feee0fe4a5763caf49))

### 📝 Documentation

* add public documentation ([81e18c1](https://github.com/mptooling/notifycat/commit/81e18c1f4680c59d60f853a32f3ac4b918b94785))
* add quickstart and operations guide ([c83b6e7](https://github.com/mptooling/notifycat/commit/c83b6e77c640a90838464a58e072b5bef0113b43))
* link to mappings.example.yaml on GitHub so mkdocs --strict resolves it ([#20](https://github.com/mptooling/notifycat/issues/20)) ([52d55f1](https://github.com/mptooling/notifycat/commit/52d55f1fca8262b0d7b7474cd65235c04d203fc8))

### 🧹 Maintenance

* add Docker image ([dcbb35c](https://github.com/mptooling/notifycat/commit/dcbb35c82aac07dd3b4a24da03740543f4c0580a))
* add just task runner ([8c3dc32](https://github.com/mptooling/notifycat/commit/8c3dc3261c9bb843cfea50eba6637ba12b0467a7))
* add test and release workflows ([ad82c9b](https://github.com/mptooling/notifycat/commit/ad82c9b35aa26a25da798c4d66f056cf75521ef2))
* full-history changelog for the first 0.1.0 release ([#21](https://github.com/mptooling/notifycat/issues/21)) ([0ba67ae](https://github.com/mptooling/notifycat/commit/0ba67ae23c6ded05f11a714b9b5e0c7eb5f13f84))
* initial project skeleton ([9d430e3](https://github.com/mptooling/notifycat/commit/9d430e3bc73c4ec329ace0ab784dedfcc3c4366d))
* pin docs deps in docs/requirements.txt so pip cache works ([#19](https://github.com/mptooling/notifycat/issues/19)) ([24d8065](https://github.com/mptooling/notifycat/commit/24d80659f7f5be27fe597ad82abd140281dcc4ed))
* set up release-please, GHCR publish, and Pages docs (closes [#13](https://github.com/mptooling/notifycat/issues/13)) ([#17](https://github.com/mptooling/notifycat/issues/17)) ([82c16fc](https://github.com/mptooling/notifycat/commit/82c16fc5ae783e28331050eec020498b69f8ae30))

### ✅ Tests

* **app:** integration tests per event type ([d91e060](https://github.com/mptooling/notifycat/commit/d91e060aa292c2922b5e35fa9315b00c7c02ad21))

## Changelog

This file is maintained automatically by
[release-please](https://github.com/googleapis/release-please) based on the
Conventional Commits history. Do not edit it by hand — new entries are written
when release-please opens or merges its release pull request.
