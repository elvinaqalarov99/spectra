<?php

namespace Specula\Laravel;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Symfony\Component\HttpFoundation\Response as SymfonyResponse;

class SpeculaMiddleware
{
    private string $endpoint;
    private array $ignore;
    private bool $captureBodies;
    private array $scrubHeaders;
    private int $maxBodyBytes;

    public function __construct()
    {
        $this->endpoint     = rtrim(config('specula.endpoint', 'http://localhost:7878'), '/');
        $this->ignore       = config('specula.ignore', []);
        $this->captureBodies = (bool) config('specula.capture_bodies', true);
        $this->scrubHeaders = config('specula.scrub_headers', ['authorization', 'cookie', 'x-api-key']);
        $this->maxBodyBytes = config('specula.max_body_bytes', 256 * 1024); // 256 KB
    }

    public function handle(Request $request, Closure $next): mixed
    {
        // Skip ignored path prefixes
        foreach ($this->ignore as $prefix) {
            if (str_starts_with($request->path(), ltrim($prefix, '/'))) {
                return $next($request);
            }
        }

        // Skip webhook paths — they use external signatures, not user auth
        if ($this->isWebhook($request)) {
            return $next($request);
        }

        $startedAt  = microtime(true);
        $response   = $next($request);
        $durationMs = (int) ((microtime(true) - $startedAt) * 1000);

        // Only capture API responses — skip redirects and HTML pages
        if (!$this->isApiResponse($response)) {
            return $response;
        }

        $this->sendObservation([
            'method'       => $request->method(),
            'rawPath'      => $this->routePath($request),
            'queryParams'  => $this->sanitizeQueryParams($request->query()),
            'requestBody'  => $this->captureRequestBody($request),
            'statusCode'   => $response->getStatusCode(),
            'responseBody' => $this->captureResponseBody($response),
            'contentType'  => $request->header('Content-Type', ''),
            'durationMs'   => $durationMs,
        ]);

        return $response;
    }

    // ── Request body ─────────────────────────────────────────────────────────

    private function captureRequestBody(Request $request): ?string
    {
        if (!$this->captureBodies) {
            return null;
        }

        // Skip multipart — binary file data, not useful for schema inference
        $contentType = $request->header('Content-Type', '');
        if (str_contains($contentType, 'multipart/form-data')) {
            // Still capture the non-file fields as JSON for schema inference
            $fields = $request->except($this->fileFieldNames($request));
            return empty($fields) ? null : json_encode($fields);
        }

        $body = $request->getContent();

        if (empty($body)) {
            return null;
        }

        // Enforce size cap
        if (strlen($body) > $this->maxBodyBytes) {
            return null;
        }

        // Only forward valid JSON
        json_decode($body);
        if (json_last_error() !== JSON_ERROR_NONE) {
            return null;
        }

        return $body;
    }

    private function fileFieldNames(Request $request): array
    {
        return array_keys($request->allFiles());
    }

    // ── Response body ────────────────────────────────────────────────────────

    private function captureResponseBody(SymfonyResponse $response): ?string
    {
        if (!$this->captureBodies) {
            return null;
        }

        // 204 No Content / 304 Not Modified — no body
        if (in_array($response->getStatusCode(), [204, 304])) {
            return null;
        }

        $body = $response->getContent();

        if (empty($body) || strlen($body) > $this->maxBodyBytes) {
            return null;
        }

        // Validate it's actually JSON
        json_decode($body);
        if (json_last_error() !== JSON_ERROR_NONE) {
            return null;
        }

        return $body;
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

    /**
     * Returns the route template with real parameter names, e.g. /users/{id}/posts/{postId}.
     * Falls back to the actual request path if no route is matched.
     */
    private function routePath(Request $request): string
    {
        $route = $request->route();
        if ($route) {
            // $route->uri() returns e.g. "v1/user/auth/login/auto/{id}/{hash}"
            return '/' . $route->uri();
        }
        return '/' . $request->path();
    }

    private function isApiResponse(SymfonyResponse $response): bool
    {
        $status = $response->getStatusCode();

        // Skip redirects — these are web routes, not API endpoints
        if ($status >= 300 && $status < 400) {
            return false;
        }

        $ct = $response->headers->get('Content-Type', '');

        // Skip HTML — definitely not an API endpoint
        if (str_contains($ct, 'text/html')) {
            return false;
        }

        // Explicit JSON content-type
        if (str_contains($ct, 'application/json') || str_contains($ct, 'application/vnd.api+json')) {
            return true;
        }

        // Sniff body shape — some apps forget to set Content-Type
        $body = $response->getContent();
        if (!empty($body)) {
            $first = ltrim($body)[0] ?? '';
            return $first === '{' || $first === '[';
        }

        // No body (e.g. 201 Created, 204 No Content) — still a valid API endpoint
        return true;
    }

    private function isWebhook(Request $request): bool
    {
        $webhookPrefixes = config('specula.webhook_prefixes', [
            'webhook', 'webhooks', 'stripe', 'spreedly', 'mailgun', 'onesignal',
        ]);
        foreach ($webhookPrefixes as $prefix) {
            if (str_contains($request->path(), $prefix)) {
                return true;
            }
        }
        return false;
    }

    private function sanitizeQueryParams(array $params): array
    {
        // Remove sensitive query params (e.g. signed URL tokens)
        $sensitive = ['signature', 'token', 'hash', 'expires'];
        return array_diff_key($params, array_flip($sensitive));
    }

    // ── Fire-and-forget delivery ─────────────────────────────────────────────

    private function sendObservation(array $obs): void
    {
        $json = json_encode($obs);
        $url  = parse_url($this->endpoint . '/ingest');
        $host = $url['host'] ?? 'localhost';
        $port = $url['port'] ?? 7878;

        $errno = $errstr = null;
        $sock  = @fsockopen($host, $port, $errno, $errstr, 0.05);
        if (!$sock) {
            return;
        }
        stream_set_blocking($sock, false);
        $payload = "POST /ingest HTTP/1.1\r\n"
            . "Host: $host:$port\r\n"
            . "Content-Type: application/json\r\n"
            . "Content-Length: " . strlen($json) . "\r\n"
            . "Connection: close\r\n\r\n"
            . $json;
        @fwrite($sock, $payload);
        @fclose($sock);
    }
}
