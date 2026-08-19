# CPA Usage Keeper CPA Plugin

CPA plugin for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)
that adds a `Keeper` resource entry to
[CPAMC](https://github.com/router-for-me/Cli-Proxy-API-Management-Center) and
opens [CPA Usage Keeper](https://github.com/Willxup/cpa-usage-keeper) inside
the management-center plugin iframe.

This plugin requires `cpa-usage-keeper` v1.12.2 or later.

The plugin does not proxy Keeper APIs and does not create Keeper sessions. It
only registers a browser resource route and embeds the configured Keeper
application URL with `embed=cpamc`.

## Related Projects

- [CPA Usage Keeper](https://github.com/Willxup/cpa-usage-keeper): the Keeper
  application embedded by this plugin.
- [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI): the CPA runtime
  and plugin host.
- [Cli-Proxy-API-Management-Center](https://github.com/router-for-me/Cli-Proxy-API-Management-Center):
  the management center that renders plugin resources.

## Configuration

Enable the plugin in CPA and configure the Keeper application root URL:

```yaml
plugins:
  enabled: true
  configs:
    keeper:
      enabled: true
      priority: 1
      keeper_url: "https://cpa.example.com/keeper/"
```

`keeper_url` must be a full `http://` or `https://` application root URL. It
must not include query parameters or fragments.

The configured Keeper instance must run `cpa-usage-keeper` v1.12.2 or later.

Examples:

```text
https://cpa.example.com/keeper/
https://keeper.example.com/
```

For the most reliable embedded login experience, deploy Keeper under the same
browser origin as CPAMC, preferably as a subpath such as `/keeper/`.

## Management API

The plugin declares the `management_api` capability and registers a single
browser resource route. It does not add JSON management endpoints and does not
proxy requests to Keeper.

## Resource Page

The plugin registers one Management API resource:

```text
GET /v0/resource/plugins/keeper/open
```

CPAMC renders plugin resources in an iframe. This resource page immediately
loads Keeper in an inner iframe with:

```text
<keeper_url>?embed=cpamc
```

If the plugin is missing `keeper_url`, or the value is invalid, the resource
page renders a small configuration message instead.

If Keeper cannot be reached, is served from the wrong base path, or runs an
older version that does not support CPAMC embed readiness, the resource page
shows a small troubleshooting message instead of exposing a browser error page.

## Cookie Boundary

Keeper owns its own authentication. Cross-site iframe login may fail when the
browser blocks third-party cookies or when Keeper is deployed with incompatible
cookie settings. This plugin does not implement a token bridge or popup login
flow.

## Build

```bash
make test
make build
make package
```

On macOS this creates:

```text
dist/keeper.dylib
dist/keeper_1.14.6_darwin_arm64.zip
dist/keeper_1.14.6_darwin_arm64.zip.sha256
```

Install locally by copying the dynamic library to CPA's plugin discovery
directory, for example:

```bash
mkdir -p /path/to/CLIProxyAPI/plugins/darwin/arm64
cp dist/keeper.dylib /path/to/CLIProxyAPI/plugins/darwin/arm64/keeper.dylib
```

Target platform, output directory, and runtime plugin version can be overridden:

```bash
make build GOOS=darwin GOARCH=arm64 BUILD_DIR=/path/to/plugins/darwin/arm64
make package VERSION=1.14.6
```

## Plugin Store Release

For plugin-store installation, each GitHub release must include:

```text
keeper_<version>_<goos>_<goarch>.zip
checksums.txt
```

The release workflow publishes the following platform archives:

Expected asset names for version `1.14.6`:

```text
keeper_1.14.6_darwin_amd64.zip
keeper_1.14.6_darwin_arm64.zip
keeper_1.14.6_freebsd_amd64.zip
keeper_1.14.6_linux_amd64.zip
keeper_1.14.6_linux_arm64.zip
keeper_1.14.6_windows_amd64.zip
keeper_1.14.6_windows_arm64.zip
checksums.txt
```

Each zip must contain the dynamic library at the zip root:

- Darwin: `keeper.dylib`
- FreeBSD: `keeper.so`
- Linux: `keeper.so`
- Windows: `keeper.dll`

`checksums.txt` must be in sha256sum format.

Generate a local aggregate checksum file with:

```bash
make checksums VERSION=1.14.6
```

After the first unified release is published, update
`router-for-me/CLIProxyAPI-Plugins-Store` so the `keeper` entry points to
`https://github.com/Willxup/cpa-usage-keeper`.

## License

This project is open source under the [MIT License](../../LICENSE).
