# spectra/laravel

> Laravel middleware for [Spectra](https://github.com/elvinaqalarov99/spectra) — API docs that can't lie.

Captures real HTTP traffic and ships observations to your local Spectra server, which builds a live OpenAPI 3.0 spec automatically. Zero annotations required.

## Requirements

- PHP 8.1+
- Laravel 10 or 11

## Installation

```bash
composer require spectra/laravel
```

The `SpectraServiceProvider` is auto-discovered by Laravel — no manual registration needed.

## Configuration

Publish the config file:

```bash
php artisan vendor:publish --tag=spectra-config
```

This creates `config/spectra.php`:

```php
return [
    'enabled'        => env('SPECTRA_ENABLED', true),
    'endpoint'       => env('SPECTRA_ENDPOINT', 'http://localhost:7878'),
    'ignore'         => ['/health', '/metrics', '/telescope'],
    'capture_bodies' => env('SPECTRA_CAPTURE_BODIES', true),
];
```

Add to your `.env`:

```env
SPECTRA_ENABLED=true
SPECTRA_ENDPOINT=http://localhost:7878
```

That's it. Open `http://localhost:7878/docs` to see your API docs populate in real time.

## Options

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Toggle observation on/off |
| `endpoint` | `http://localhost:7878` | Spectra server URL |
| `ignore` | `['/health', ...]` | Path prefixes to skip |
| `capture_bodies` | `true` | Include request/response bodies |

## How it works

The middleware runs after the response is built, then sends the full request/response pair to the Spectra server's `/ingest` endpoint via a non-blocking raw socket. Response time is not affected. If the Spectra server is unreachable, the observation is silently dropped.

## Starting the Spectra server

```bash
# Download the binary
curl -sSL https://github.com/elvinaqalarov99/spectra/releases/latest/download/spectra-darwin-arm64 -o spectra
chmod +x spectra

# Start — proxy on :9999, docs on :7878
./spectra start --target http://localhost:3000
```

## License

MIT © [Elvin Agalarov](https://github.com/elvinaqalarov99)
