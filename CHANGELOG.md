# Changelog

## [0.1.0-alpha](https://github.com/Klazomenai/bridge/compare/v0.0.1...v0.1.0-alpha) (2026-03-29)


### Added

* add release-please and Docker image release pipeline ⛵ ([#71](https://github.com/Klazomenai/bridge/issues/71)) ([7ab8725](https://github.com/Klazomenai/bridge/commit/7ab8725fbacc1d2a1ea2df00c4b4cf4fd3971f6f))
* **bot:** async message handling with typing indicators ✨ ([#62](https://github.com/Klazomenai/bridge/issues/62)) ([d38824f](https://github.com/Klazomenai/bridge/commit/d38824f15ef8052f1aaefd79cd55178ccb8c0942)), closes [#36](https://github.com/Klazomenai/bridge/issues/36)
* **crew:** add Bosun and Lookout crew members ⛵ ([#64](https://github.com/Klazomenai/bridge/issues/64)) ([9d030d1](https://github.com/Klazomenai/bridge/commit/9d030d19c77b1e0c4d0235cdc80c95e631b2e897)), closes [#37](https://github.com/Klazomenai/bridge/issues/37)
* **crew:** add Chips the Carpenter crew member ⛵ ([#65](https://github.com/Klazomenai/bridge/issues/65)) ([72dec80](https://github.com/Klazomenai/bridge/commit/72dec80cc96019f6ded46fba66be3ec28dbbd3d0)), closes [#55](https://github.com/Klazomenai/bridge/issues/55)
* implement Go bridge bot with mautrix-go and Anthropic crew ([#10](https://github.com/Klazomenai/bridge/issues/10)) ([f391981](https://github.com/Klazomenai/bridge/commit/f391981c93f49f3abb035dbce702f1f685d55f65))
* **orchestrator:** implement crew-to-crew delegation tool ✨ ([#60](https://github.com/Klazomenai/bridge/issues/60)) ([7d22111](https://github.com/Klazomenai/bridge/commit/7d2211148092d3a66529bf2dfb0883cb09e91148)), closes [#35](https://github.com/Klazomenai/bridge/issues/35)
* **orchestrator:** implement tool-use loop ([#47](https://github.com/Klazomenai/bridge/issues/47)) ([911dcb3](https://github.com/Klazomenai/bridge/commit/911dcb3ac836d40343d00498e20f58b5fbb3a9df)), closes [#32](https://github.com/Klazomenai/bridge/issues/32)
* **tools:** extract tool execution sandbox with panic recovery ✨ ([#59](https://github.com/Klazomenai/bridge/issues/59)) ([ea2ae1d](https://github.com/Klazomenai/bridge/commit/ea2ae1dfd12d5b8176201cae8425be1d6631392d)), closes [#33](https://github.com/Klazomenai/bridge/issues/33)
* **tools:** implement Chips read-only GitHub tools ⛵ ([#69](https://github.com/Klazomenai/bridge/issues/69)) ([78d5791](https://github.com/Klazomenai/bridge/commit/78d5791fe385f8978e1581dec6723f55592cc96f)), closes [#56](https://github.com/Klazomenai/bridge/issues/56)
* **tools:** implement Crest email tools (imap_poll, smtp_send) ([#48](https://github.com/Klazomenai/bridge/issues/48)) ([96af218](https://github.com/Klazomenai/bridge/commit/96af218d421477e88a28517218847ea1c4fd6de6)), closes [#34](https://github.com/Klazomenai/bridge/issues/34)
* **tools:** implement Lookout monitoring tools ⛵ ([#67](https://github.com/Klazomenai/bridge/issues/67)) ([82965ec](https://github.com/Klazomenai/bridge/commit/82965eca03054d534dbea6cc5aa75b6367b3ac3d))
* **tools:** implement Maren cluster read tools ⛵ ([#66](https://github.com/Klazomenai/bridge/issues/66)) ([3b6c71c](https://github.com/Klazomenai/bridge/commit/3b6c71c3ebeab86aae2d29dbcaf0321a3522955d))
* **tools:** implement tool registry and executor interface ([#46](https://github.com/Klazomenai/bridge/issues/46)) ([ef57125](https://github.com/Klazomenai/bridge/commit/ef571259528ebd7906177e9451da56bbc7e9fd49)), closes [#31](https://github.com/Klazomenai/bridge/issues/31)


### Fixed

* create mount point directories in distroless image and align crypto store path ([#13](https://github.com/Klazomenai/bridge/issues/13)) ([5642334](https://github.com/Klazomenai/bridge/commit/5642334c749d77cc1e0b872ffccc75f9b1156b74))


### Changed

* **orchestrator,bot:** split packages for M2 tool-use readiness ([#45](https://github.com/Klazomenai/bridge/issues/45)) ([a41f7ec](https://github.com/Klazomenai/bridge/commit/a41f7ecd062d0dfffadb10313219b311829c46a7)), closes [#44](https://github.com/Klazomenai/bridge/issues/44)
