# [1.6.0](https://github.com/wu/keyop/compare/v1.5.0...v1.6.0) (2026-09-25)


### Bug Fixes

* add additional keywords for markdown highlighting ([cc35695](https://github.com/wu/keyop/commit/cc35695983cf02783ac4b9a57eaed60c341498f3))
* add zsh to markdown highlight keywords ([d1f0b0d](https://github.com/wu/keyop/commit/d1f0b0d706fa3c6daaa7278bd6f726f15218b458))
* fail startup when an enabled plugin's .so is missing ([5dd8617](https://github.com/wu/keyop/commit/5dd86178eb37392010a34e02655725ade5cb0107))
* **federation:** make coordinator/reader close idempotent under concurrency ([9538a57](https://github.com/wu/keyop/commit/9538a5709beaab5805e9a10f4a995cb77c24b5b1))
* update go.sum to include missing go.mod entries for several dependencies ([9b212e3](https://github.com/wu/keyop/commit/9b212e3a41f6d184ef6568f4aa09040aa53a6e78))


### Features

* add 'npx' to highlight extensions list for improved command recognition ([f691d8d](https://github.com/wu/keyop/commit/f691d8d389ff0a0950bcd2f21798f629d2b37e6c))
* add additional command highlights to highlight_extensions.go ([9d128ea](https://github.com/wu/keyop/commit/9d128ea0923ff975a0e570fe54998bc3aa27b6f2))
* add keyop-messenger to highlight extensions ([7d1e14d](https://github.com/wu/keyop/commit/7d1e14d5d94610ece12ff0ba4e6f31a668717b26))
* add launchd subcommand for macOS LaunchAgent management ([a130623](https://github.com/wu/keyop/commit/a130623b48743e41a06115ed200e7cd5a5514476))
* add new service configurations for CPU, memory, heartbeat, and log management ([fc8d914](https://github.com/wu/keyop/commit/fc8d91495c77a05814d35dc4a7917ea861338cbd))
* add NotifyHints delivery hints to AlertEvent ([443adc5](https://github.com/wu/keyop/commit/443adc5bd6b885ff936ad7bd986a02aa871b707f))
* add optional Link field to AlertEvent for related content navigation ([0b871f1](https://github.com/wu/keyop/commit/0b871f1147294a1bff814fecfa0cb2f394ee549a))
* add optional SQLiteMultiInserter for multi-statement inserts ([df5f3ea](https://github.com/wu/keyop/commit/df5f3eaf678b8dc2e28f6656be72e8d494012b5f))
* add self-update functionality and support for multiple ARM architectures ([6f203ab](https://github.com/wu/keyop/commit/6f203abbc3e2d262b29e82e4c5d1f42d90aa0c75))
* add SummarizeChanges for human-readable change summaries ([0f2732f](https://github.com/wu/keyop/commit/0f2732f9656de87f01085dd126c942a11e3022de))
* add synchronization to mockStateStore and update runCount handling in tests ([9570b1f](https://github.com/wu/keyop/commit/9570b1f5c9bf746150ff058dc164327230e91a36))
* add validate-config subcommand for pre-restart config validation ([2d42ebe](https://github.com/wu/keyop/commit/2d42ebeffc5c55b2350b1e369ef9df2c960c903a))
* apply config rules on publish and on delivery ([9f7a7d7](https://github.com/wu/keyop/commit/9f7a7d734be5292936de9fbd88f416b8de3107bc))
* attribute CPU profile samples to individual services ([783d20d](https://github.com/wu/keyop/commit/783d20de6bd329210ca6b4350357436426d1970f))
* **core:** add optional SQLiteAfterInserter hook ([07c5a84](https://github.com/wu/keyop/commit/07c5a842d75547dc72b9df8f56551b3288a883da))
* **core:** let index providers report bulk index failures ([fe66dea](https://github.com/wu/keyop/commit/fe66deab33d8b75253c976926d250bf62307a14d))
* **core:** let rules add to list fields and interpolate values ([74e538e](https://github.com/wu/keyop/commit/74e538e8d2c799839c93f8dace0b2b742e9b4f2f))
* enhance markdown rendering with math expression support and GitHub alert styling ([c6f8979](https://github.com/wu/keyop/commit/c6f8979d4457096d814620aa18ace2289498683c))
* enhance messenger initialization with logger support ([f173b80](https://github.com/wu/keyop/commit/f173b8088aa287cbce1daffa115d02613bc2b858))
* enhance PreprocessWikiLinks to support source:id format for markdown links ([5785dba](https://github.com/wu/keyop/commit/5785dba16878010a68181ca0f7ec993e5262b21f))
* **heartbeat:** report the running keyop version ([44a56a7](https://github.com/wu/keyop/commit/44a56a715504dd18c369f54a3aa40ecc18200f13))
* implement Delete method for FileStateStore and add corresponding tests ([67e07e4](https://github.com/wu/keyop/commit/67e07e414b815b52fb106b35c2a3467a62fd60be))
* improve code block protection in Markdown rendering to handle nested lists and inline code correctly ([a9466ad](https://github.com/wu/keyop/commit/a9466adaf62c393f24d149678d100afb19fa3866))
* load and validate config rules at startup ([9960ab1](https://github.com/wu/keyop/commit/9960ab109ad234b79a75d0852a9deb0b59466c8d))
* **messenger:** apply per-channel subscribe options from config automatically ([2dc723d](https://github.com/wu/keyop/commit/2dc723d6388cc73d8d677b2d211849ee0fe8cd52))
* name the payload-registrar interface and match runtime hook ([6939934](https://github.com/wu/keyop/commit/6939934899f73701974349a59db54837dfa21b68))
* numerous rss performance improvements and fixes ([c9899a7](https://github.com/wu/keyop/commit/c9899a732888743a4a4e05aa2569d1fe1cd98375))
* **runtime:** exit non-zero when a hub connection fails permanently ([f116882](https://github.com/wu/keyop/commit/f116882ce8bc1b136ad38906e56645c7958ca3e2))
* **sqlite:** time every SQLite statement into a dedicated log ([5eb7f8e](https://github.com/wu/keyop/commit/5eb7f8e6216a507745d0a1a2588f6a15133bc4d4))
* **systemctl:** add --home flag to pin HOME and WorkingDirectory ([ac6494c](https://github.com/wu/keyop/commit/ac6494cebf137fc55b76034c00b40379837cef4f))
* update GitHub Actions conditions to skip non-GitHub forges and semantic-release commits ([d786f61](https://github.com/wu/keyop/commit/d786f6181e76e29a093f4495864d9dacbdc4750c))
* update keyop-messenger to v1.3.0 and add MessageID to InsertContext ([390e668](https://github.com/wu/keyop/commit/390e66875cfe93176404a0c85fdc794662c3088e))
* update list of highlighted commands ([8b38849](https://github.com/wu/keyop/commit/8b38849ca15411a2087f2855f19b8d19b6940555))
* update Subscribe method to accept optional SubscribeOptions ([0aa2dab](https://github.com/wu/keyop/commit/0aa2dab23b68f583e9008ac6fa3f57e9c415ef4b))
* **util:** link [[yyyy-mm-dd]] and [[yyyy.mm.dd]] to the journal ([652888c](https://github.com/wu/keyop/commit/652888c27431a110ce0806fdc7938d01eb57aa51))

# [1.5.0](https://github.com/wu/keyop/compare/v1.4.0...v1.5.0) (2026-05-14)


### Features

* git identity configuration for journal autocommit ([f2164d7](https://github.com/wu/keyop/commit/f2164d7c935e876a6bb5dfd61e185c629b1d1d5f))

# [1.4.0](https://github.com/wu/keyop/compare/v1.3.0...v1.4.0) (2026-05-11)


### Features

* add git operations utility functions for repository management ([ed64a91](https://github.com/wu/keyop/commit/ed64a9133a39f08485aa53c1fefbea1b9f25c2cb))
* statusmon now publishes state event if enabled ([7f7d240](https://github.com/wu/keyop/commit/7f7d2403151b1bbb4d71159ddab007ab8088e220))

# [1.3.0](https://github.com/wu/keyop/compare/v1.2.0...v1.3.0) (2026-05-03)


### Features

* add copilot instructions and policies for repository management ([0f61b3c](https://github.com/wu/keyop/commit/0f61b3c871fa273c4ab457798b4d2a3768b32c43))
* add MCP provider interface ([b1bf34b](https://github.com/wu/keyop/commit/b1bf34bb7032ea6ef10849043a6e1c5e01a4913b))
* embed source temp event in metric event ([01b4cb6](https://github.com/wu/keyop/commit/01b4cb6a48d72bdd445726ae17eed516dfa07b55))
* status event embeds source event in payload ([fa621d9](https://github.com/wu/keyop/commit/fa621d96e8e664d5faa3de1cecbe06d054741352))

# [1.2.0](https://github.com/wu/keyop/compare/v1.1.0...v1.2.0) (2026-05-01)


### Bug Fixes

* improve error message formatting in task failure reporting ([72e1cbb](https://github.com/wu/keyop/commit/72e1cbb237e05a2dc94f17887688dcec4e9504c3))


### Features

* add release target to Makefile for pre-release checks ([a54b201](https://github.com/wu/keyop/commit/a54b201dd446c54e06bebc252c252076bd739fec))
* implement RotatingFileWriter for daily log rotation ([5deb403](https://github.com/wu/keyop/commit/5deb403c2c396e7a5af7fab5e962ce7417aefaed))

# [1.1.0](https://github.com/wu/keyop/compare/v1.0.1...v1.1.0) (2026-04-25)


### Bug Fixes

* update keyop-messenger dependency to v0.13.0 and clean up go.mod ([9e152ac](https://github.com/wu/keyop/commit/9e152acac1a273eb4aeadd175ddeed8df014664a))


### Features

* update keyop-messenger to version 0.14.0 ([9d603f2](https://github.com/wu/keyop/commit/9d603f2b7e83301218b0ea5c7704f01e1b486834))

## [1.0.1](https://github.com/wu/keyop/compare/v1.0.0...v1.0.1) (2026-04-23)


### Bug Fixes

* resolve issues with two failing tests ([a766aaf](https://github.com/wu/keyop/commit/a766aaf230ea2d77d563354bb319cee0a87e6fbf))

# 1.0.0 (2026-04-23)


### Features

* extract public keyop repo ([65c3709](https://github.com/wu/keyop/commit/65c37093df5591b30337afbc6890912f2e486b8c))
