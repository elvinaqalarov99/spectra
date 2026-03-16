<?php

namespace Specula\Laravel;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\Support\Facades\Log;
use Symfony\Component\HttpFoundation\Response as SymfonyResponse;

class SpeculaMiddleware
{
    private string $endpoint;
    private array $ignore;
    private bool $captureBodies;
    private array $scrubHeaders;
    private int $maxBodyBytes;
    private bool $debug;
    private string $stripPrefix;

    public function __construct()
    {
        $this->endpoint      = rtrim(config('specula.endpoint', 'http://localhost:7878'), '/');
        $this->ignore        = config('specula.ignore', []);
        $this->captureBodies = (bool) config('specula.capture_bodies', true);
        $this->scrubHeaders  = config('specula.scrub_headers', ['authorization', 'cookie', 'x-api-key']);
        $this->maxBodyBytes  = config('specula.max_body_bytes', 256 * 1024);
        $this->debug         = (bool) config('specula.debug', false);
        $this->stripPrefix   = config('specula.strip_prefix', 'v1');
    }

    public function handle(Request $request, Closure $next): mixed
    {
        $path = $request->path();

        // Skip ignored path prefixes
        foreach ($this->ignore as $prefix) {
            if (str_starts_with($path, ltrim($prefix, '/'))) {
                $this->log("SKIP ignored prefix '$prefix' → $path");
                return $next($request);
            }
        }

        // Skip webhook paths
        if ($this->isWebhook($request)) {
            $this->log("SKIP webhook → $path");
            return $next($request);
        }

        $startedAt  = microtime(true);
        $response   = $next($request);
        $durationMs = (int) ((microtime(true) - $startedAt) * 1000);

        $status = $response->getStatusCode();
        $ct     = $response->headers->get('Content-Type', '(none)');

        // Only capture API responses — skip HTML pages
        if (!$this->isApiResponse($response)) {
            $this->log("SKIP non-API response [{$status}] Content-Type: {$ct} → $path");
            return $response;
        }

        $routePath = $this->routePath($request);
        $this->log("TRACK [{$status}] {$request->method()} $routePath  Content-Type: {$ct}");

        $sent = $this->sendObservation([
            'method'          => $request->method(),
            'rawPath'         => $routePath,
            'queryParams'     => (object) $this->sanitizeQueryParams($request->query()),
            'requestBody'     => $this->captureRequestBody($request),
            'statusCode'      => $status,
            'responseBody'    => $this->captureResponseBody($response),
            'responseHeaders' => (object) $this->captureResponseHeaders($response),
            'contentType'     => $request->header('Content-Type', ''),
            'durationMs'      => $durationMs,
            'stripPrefix'     => $this->stripPrefix,
        ]);

        if (!$sent) {
            $this->log("WARN socket delivery failed for $routePath — is Specula running at {$this->endpoint}?");
        }

        return $response;
    }

    // ── Request body ─────────────────────────────────────────────────────────

    private function captureRequestBody(Request $request): ?string
    {
        if (!$this->captureBodies) {
            return null;
        }

        $contentType = $request->header('Content-Type', '');
        if (str_contains($contentType, 'multipart/form-data')) {
            // Capture non-file fields normally; mark file fields with sentinel
            // so the Go merger can document them as format: binary
            $fields = $request->except($this->fileFieldNames($request));
            foreach ($this->fileFieldNames($request) as $name) {
                $fields[$name] = '__file__';
            }
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

    private function captureResponseHeaders(SymfonyResponse $response): array
    {
        $headers = [];
        // Capture Location for redirects — documents where the endpoint redirects to
        $location = $response->headers->get('Location');
        if ($location !== null) {
            $headers['Location'] = $location;
        }
        return $headers;
    }

    private function isApiResponse(SymfonyResponse $response): bool
    {
        // Only skip HTML pages — everything else is an API endpoint worth documenting:
        // JSON responses, file downloads (image/png, text/csv, etc.), empty-body 2xx,
        // and redirects (302 with Location header) are all tracked.
        $ct = $response->headers->get('Content-Type', '');
        return !str_contains($ct, 'text/html');
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

    private function sendObservation(array $obs): bool
    {
        $json = json_encode($obs);
        if ($json === false) {
            return false;
        }

        $url = $this->endpoint . '/ingest';

        // Use curl — far more reliable than raw fsockopen for HTTP delivery.
        // 50ms connect timeout, 300ms total. We never read the response.
        $ch = curl_init($url);
        if (!$ch) {
            return false;
        }

        curl_setopt_array($ch, [
            CURLOPT_POST           => true,
            CURLOPT_POSTFIELDS     => $json,
            CURLOPT_HTTPHEADER     => ['Content-Type: application/json', 'Expect:'],
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_NOSIGNAL       => true,   // safe in multi-threaded PHP-FPM
            CURLOPT_CONNECTTIMEOUT_MS => 50,
            CURLOPT_TIMEOUT_MS     => 300,
        ]);

        curl_exec($ch);
        $ok = curl_errno($ch) === 0;
        curl_close($ch);

        return $ok;
    }

    // ── Debug logging ────────────────────────────────────────────────────────

    private function log(string $message): void
    {
        if ($this->debug) {
            Log::channel('single')->debug("[Specula] $message");
        }
    }
}
