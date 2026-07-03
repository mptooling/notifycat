# Changelog

## [0.21.0](https://github.com/mptooling/notifycat/compare/v0.20.1...v0.21.0) (2026-07-03)


### 🚀 Features

* clear the in-review state on review submit ([#153](https://github.com/mptooling/notifycat/issues/153)) ([91a4038](https://github.com/mptooling/notifycat/commit/91a40382031b53c1d37431e1dcf20f1519c9dc76))
* code_reviews store and migration for active review ownership ([#141](https://github.com/mptooling/notifycat/issues/141)) ([d1b6905](https://github.com/mptooling/notifycat/commit/d1b6905a48f91a7f92095e187bd2847b403347f8))
* finish review sessions on submit and show reviewers on close ([#144](https://github.com/mptooling/notifycat/issues/144)) ([6e8bec0](https://github.com/mptooling/notifycat/commit/6e8bec0913fd1f10073535262b12d92309427ebe))
* handle the "Start review" click — record reviewer and append marker ([#143](https://github.com/mptooling/notifycat/issues/143)) ([290eeaf](https://github.com/mptooling/notifycat/commit/290eeaf1dc13a11b0366e829e7e204cfbcd18cad))
* inbound Slack interactivity endpoint (signed callback foundation) ([#138](https://github.com/mptooling/notifycat/issues/138)) ([2e6603e](https://github.com/mptooling/notifycat/commit/2e6603e415c20b0710107e833c21e3c1c40d3200))
* open the PR page when the Start review button is clicked ([#145](https://github.com/mptooling/notifycat/issues/145)) ([505bec9](https://github.com/mptooling/notifycat/commit/505bec92c346be3af789ee1b291998e672e6d27b))
* render the "Start review" button and "In review" message state ([#142](https://github.com/mptooling/notifycat/issues/142)) ([a55cdf2](https://github.com/mptooling/notifycat/commit/a55cdf24e944b090c3bde34aa6d9f4b1c6b1ec5c))


### ♻️ Refactors

* reduce duplication and decompose large functions ([#140](https://github.com/mptooling/notifycat/issues/140)) ([bfa5293](https://github.com/mptooling/notifycat/commit/bfa529349372c95d339b9bcb564eba12ac8ca998))

## [0.20.1](https://github.com/mptooling/notifycat/compare/v0.20.0...v0.20.1) (2026-06-30)


### 🐛 Bug fixes

* delete synthetic pull_requests row after smoke run ([#137](https://github.com/mptooling/notifycat/issues/137)) ([f7f8b08](https://github.com/mptooling/notifycat/commit/f7f8b086de2c71baa6b6d7fdefebccab629950c3))


### 🧹 Maintenance

* **deps:** bump gorm.io/gorm from 1.31.1 to 1.31.2 in the go-modules group ([#135](https://github.com/mptooling/notifycat/issues/135)) ([d327110](https://github.com/mptooling/notifycat/commit/d327110d566809129636338941d7ecd37fd1da98))

## [0.20.0](https://github.com/mptooling/notifycat/compare/v0.19.0...v0.20.0) (2026-06-30)


### 🚀 Features

* per-path mappings config schema and validation ([#128](https://github.com/mptooling/notifycat/issues/128)) ([aa64f02](https://github.com/mptooling/notifycat/commit/aa64f02832fda14f356adfedcde3dd9f1d58026c))
* per-path mappings single-winner resolution and token-gating warnings ([#130](https://github.com/mptooling/notifycat/issues/130)) ([1b041f6](https://github.com/mptooling/notifycat/commit/1b041f670575e84fdcc1e4c00720fed89c594189))
* per-path routing runtime — fetch changed files and route to path channel ([#131](https://github.com/mptooling/notifycat/issues/131)) ([ea919bc](https://github.com/mptooling/notifycat/commit/ea919bcd17c2d08596f08e49c86540c8323c1d08))
* per-PR multi-message fan-out across path channels ([#133](https://github.com/mptooling/notifycat/issues/133)) ([932d92f](https://github.com/mptooling/notifycat/commit/932d92fd6d4da8ca391515bd9b6bb34b06970052))
* pull_requests and messages store foundation ([#132](https://github.com/mptooling/notifycat/issues/132)) ([f5b9f1a](https://github.com/mptooling/notifycat/commit/f5b9f1a866eb2cf09f186e5ae2849d9c41d52e62))

## [0.19.0](https://github.com/mptooling/notifycat/compare/v0.18.0...v0.19.0) (2026-06-26)


### 🚀 Features

* configurable digest timezone (default UTC) ([#122](https://github.com/mptooling/notifycat/issues/122)) ([b013ec2](https://github.com/mptooling/notifycat/commit/b013ec201dd63210be39203cad9c69c2f90ebe22))

## [0.18.0](https://github.com/mptooling/notifycat/compare/v0.17.0...v0.18.0) (2026-06-26)


### ⚠ BREAKING CHANGES

* per-repo mapping configuration ([#113](https://github.com/mptooling/notifycat/issues/113))

### 🚀 Features

* per-repo mapping configuration ([#113](https://github.com/mptooling/notifycat/issues/113)) ([26a4383](https://github.com/mptooling/notifycat/commit/26a438363db0c4dc2930733acbf70e3041944cdd))

## [0.17.0](https://github.com/mptooling/notifycat/compare/v0.16.0...v0.17.0) (2026-06-24)


### ⚠ BREAKING CHANGES

* consolidate non-secret configuration into a single config.yaml ([#109](https://github.com/mptooling/notifycat/issues/109))

### 🚀 Features

* consolidate non-secret configuration into a single config.yaml ([#109](https://github.com/mptooling/notifycat/issues/109)) ([d2f4d6a](https://github.com/mptooling/notifycat/commit/d2f4d6a50a70d1d13194b4a6108fefddb35dbd27))


### 🧹 Maintenance

* bump minor (not major) for pre-1.0 breaking changes ([#112](https://github.com/mptooling/notifycat/issues/112)) ([a18653c](https://github.com/mptooling/notifycat/commit/a18653c760c25867b0c76a69f7e3896f89e750e6))

## [0.16.0](https://github.com/mptooling/notifycat/compare/v0.15.4...v0.16.0) (2026-06-24)


### 🚀 Features

* remind on stuck PRs with a daily per-channel digest ([#100](https://github.com/mptooling/notifycat/issues/100)) ([2c02199](https://github.com/mptooling/notifycat/commit/2c02199b23f11192d0c877ba5d25638ef226b4f1))

## [0.15.4](https://github.com/mptooling/notifycat/compare/v0.15.3...v0.15.4) (2026-06-24)


### 🧹 Maintenance

* **deps:** bump actions/checkout from 6 to 7 ([#107](https://github.com/mptooling/notifycat/issues/107)) ([6a206dd](https://github.com/mptooling/notifycat/commit/6a206dd48479716fd17c18bdd4623ecc3374d42e))
* **deps:** bump amannn/action-semantic-pull-request from 5 to 6 ([#103](https://github.com/mptooling/notifycat/issues/103)) ([4f14554](https://github.com/mptooling/notifycat/commit/4f14554b6cee550093170ae4676961eea3fd6d99))
* **deps:** bump docker/setup-buildx-action from 3 to 4 ([#102](https://github.com/mptooling/notifycat/issues/102)) ([13ddd7b](https://github.com/mptooling/notifycat/commit/13ddd7b2ab5496079c1a13aaa439193a404f7a52))
* **deps:** bump docker/setup-qemu-action from 3 to 4 ([#101](https://github.com/mptooling/notifycat/issues/101)) ([ddd4e04](https://github.com/mptooling/notifycat/commit/ddd4e04bd8e9d8a93a371f18e981831a438ac3ec))

## [0.15.3](https://github.com/mptooling/notifycat/compare/v0.15.2...v0.15.3) (2026-06-11)


### 🧹 Maintenance

* **deps:** bump actions/configure-pages from 5 to 6 ([#104](https://github.com/mptooling/notifycat/issues/104)) ([2962490](https://github.com/mptooling/notifycat/commit/2962490d6092be87e296e965e60a5af6758f7955))

## [0.15.2](https://github.com/mptooling/notifycat/compare/v0.15.1...v0.15.2) (2026-06-06)


### 🧹 Maintenance

* publish beta container image for each PR ([#98](https://github.com/mptooling/notifycat/issues/98)) ([2cdb56f](https://github.com/mptooling/notifycat/commit/2cdb56feff917ab0315038f35e813bf27f61073b))

## [0.15.1](https://github.com/mptooling/notifycat/compare/v0.15.0...v0.15.1) (2026-06-06)


### 🧹 Maintenance

* add PR labeler, size-labeler, auto-assign, and stale workflows ([#94](https://github.com/mptooling/notifycat/issues/94)) ([f35df36](https://github.com/mptooling/notifycat/commit/f35df364eef1ac8764afdf43f221ffb2431aba2e))
* pin auto-assign-action to v2.0.2 ([#97](https://github.com/mptooling/notifycat/issues/97)) ([167f8ad](https://github.com/mptooling/notifycat/commit/167f8ad3ee933c9705896deca72db76516e1b561))

## [0.15.0](https://github.com/mptooling/notifycat/compare/v0.14.0...v0.15.0) (2026-06-05)


### 🚀 Features

* render PR Slack notifications as Block Kit with repo, author, and open time ([#92](https://github.com/mptooling/notifycat/issues/92)) ([4883ada](https://github.com/mptooling/notifycat/commit/4883adadf998858efe89f4c7c4e67b51d9f513aa))

## [0.14.0](https://github.com/mptooling/notifycat/compare/v0.13.1...v0.14.0) (2026-06-05)


### 🧹 Maintenance

* **deps:** bump actions/checkout from 5 to 6 ([#84](https://github.com/mptooling/notifycat/issues/84)) ([06e6933](https://github.com/mptooling/notifycat/commit/06e69337862bac74a3ba29944b7536ef6c127b51))
* **deps:** bump actions/deploy-pages from 4 to 5 ([#88](https://github.com/mptooling/notifycat/issues/88)) ([9781bff](https://github.com/mptooling/notifycat/commit/9781bffe2b08432500b4fde697e442ab4351ca66))
* **deps:** bump actions/setup-python from 5 to 6 ([#86](https://github.com/mptooling/notifycat/issues/86)) ([a416db0](https://github.com/mptooling/notifycat/commit/a416db059de399eec63524ae33e0f4dc45461ea4))
* **deps:** bump actions/upload-pages-artifact from 3 to 5 ([#85](https://github.com/mptooling/notifycat/issues/85)) ([d6651b9](https://github.com/mptooling/notifycat/commit/d6651b92545fb9859315241dae2977e2b4c1842f))
* **deps:** bump golang from 1.25.11-alpine to 1.26.4-alpine in the docker group ([#83](https://github.com/mptooling/notifycat/issues/83)) ([fe2ec67](https://github.com/mptooling/notifycat/commit/fe2ec67cb8b628f8dc0daee6657d80ebda064412))
* **deps:** bump googleapis/release-please-action from 4 to 5 ([#87](https://github.com/mptooling/notifycat/issues/87)) ([26add62](https://github.com/mptooling/notifycat/commit/26add62652532a1ad8e699aa5ba05b66dcb67384))
* release 0.14.0 ([74d5a4b](https://github.com/mptooling/notifycat/commit/74d5a4b718b8b09bcc73e16f3207fc21800e5c0f))

## [0.13.1](https://github.com/mptooling/notifycat/compare/v0.13.0...v0.13.1) (2026-06-05)


### 🐛 Bug fixes

* classify bot PRs by author so compact format applies on ready_for_review ([#89](https://github.com/mptooling/notifycat/issues/89)) ([7205091](https://github.com/mptooling/notifycat/commit/72050919184377ac372450c6950b05aa0cd62949))

## [0.13.0](https://github.com/mptooling/notifycat/compare/v0.12.1...v0.13.0) (2026-06-05)


### 🚀 Features

* compact Slack format for Dependabot and Renovate PRs ([#79](https://github.com/mptooling/notifycat/issues/79)) ([3108b02](https://github.com/mptooling/notifycat/commit/3108b0259e5b1b2ce1c311bb4afec6368adff668))


### 🧹 Maintenance

* enable dependabot version updates for go modules, actions, and docker ([#81](https://github.com/mptooling/notifycat/issues/81)) ([fd233bc](https://github.com/mptooling/notifycat/commit/fd233bca83b81e45c7b302a4b92019fa6dc9f3cc))

## [0.12.1](https://github.com/mptooling/notifycat/compare/v0.12.0...v0.12.1) (2026-06-05)


### 🐛 Bug fixes

* **docs:** use static MIT license badge to avoid intermittent 'invalid' ([#77](https://github.com/mptooling/notifycat/issues/77)) ([4c569ab](https://github.com/mptooling/notifycat/commit/4c569ab03ff2c4bcd182288bd09b076031c75d77))

## [0.12.0](https://github.com/mptooling/notifycat/compare/v0.11.3...v0.12.0) (2026-06-05)


### 🚀 Features

* distinct reaction for non-suppressed bot reviews (closes [#5](https://github.com/mptooling/notifycat/issues/5)) ([#75](https://github.com/mptooling/notifycat/issues/75)) ([98f9923](https://github.com/mptooling/notifycat/commit/98f9923dc0f6450b766438836f1e32c8c756930d))

## [0.11.3](https://github.com/mptooling/notifycat/compare/v0.11.2...v0.11.3) (2026-06-05)


### 📝 Documentation

* headline the one-command quickstart in README and docs nav ([#73](https://github.com/mptooling/notifycat/issues/73)) ([a90d58e](https://github.com/mptooling/notifycat/commit/a90d58e34d19ee1c3fe236769228905395d485a4))

## [0.11.2](https://github.com/mptooling/notifycat/compare/v0.11.1...v0.11.2) (2026-06-05)


### 🐛 Bug fixes

* **release:** publish env template as env.example so the install bundle works ([#71](https://github.com/mptooling/notifycat/issues/71)) ([f18cfcd](https://github.com/mptooling/notifycat/commit/f18cfcd00eaed987cdbd8429fbaf1a0d0254fd7b))

## [0.11.1](https://github.com/mptooling/notifycat/compare/v0.11.0...v0.11.1) (2026-06-05)


### 🧹 Maintenance

* attach compose files, checksums, and versioned install URL to releases ([#69](https://github.com/mptooling/notifycat/issues/69)) ([cc9390d](https://github.com/mptooling/notifycat/commit/cc9390d68b13f40a1db8f657a902920ba4114f2b))

## [0.11.0](https://github.com/mptooling/notifycat/compare/v0.10.0...v0.11.0) (2026-06-05)


### 🚀 Features

* **doctor:** check the public webhook URL derived from DOMAIN ([#65](https://github.com/mptooling/notifycat/issues/65)) ([2ea33cb](https://github.com/mptooling/notifycat/commit/2ea33cb8b137402f73700923f227d8009d342bdf))
* notifycat smoke — local PR-event delivery test ([#67](https://github.com/mptooling/notifycat/issues/67)) ([554d3eb](https://github.com/mptooling/notifycat/commit/554d3ebd4f418213a0bf5003719a19b789dcf6ac))


### 📝 Documentation

* add install-path security and permissions checklist ([#68](https://github.com/mptooling/notifycat/issues/68)) ([634130b](https://github.com/mptooling/notifycat/commit/634130b84716999653f98578502f62aa3dc07281))

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
