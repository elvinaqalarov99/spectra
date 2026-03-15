# @spectra/nestjs

> NestJS middleware for [Spectra](https://github.com/elvinaqalarov99/spectra) — API docs that can't lie.

Captures real HTTP traffic and ships observations to your local Spectra server, which builds a live OpenAPI 3.0 spec automatically. Zero annotations required.

## Installation

```bash
npm install @spectra/nestjs
```

## Usage

```typescript
// main.ts
import { NestFactory } from '@nestjs/core';
import { SpectraModule } from '@spectra/nestjs';
import { AppModule } from './app.module';

async function bootstrap() {
  const app = await NestFactory.create(AppModule);

  app.use(SpectraModule.middleware({
    endpoint: 'http://localhost:7878',  // your running Spectra server
    ignore: ['/health', '/metrics'],
    captureBodies: true,
  }));

  await app.listen(3000);
}
bootstrap();
```

That's it. Open `http://localhost:7878/docs` to see your API docs populate in real time.

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `endpoint` | `string` | `http://localhost:7878` | Spectra server URL |
| `ignore` | `string[]` | `['/health', '/metrics', '/favicon.ico']` | Path prefixes to skip |
| `captureBodies` | `boolean` | `true` | Include request/response bodies in observations |

## How it works

The middleware wraps `res.write` and `res.end` to capture the response body, then sends the full request/response pair to the Spectra server's `/ingest` endpoint via a fire-and-forget `fetch` call with a 2-second timeout. If the Spectra server is unreachable the error is swallowed silently — your production traffic is never affected.

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
