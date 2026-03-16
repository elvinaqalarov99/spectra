<?php

return [
    /*
    |--------------------------------------------------------------------------
    | Enable Specula observation
    |--------------------------------------------------------------------------
    | Set to false in production. Specula is a development tool.
    */
    'enabled' => env('SPECULA_ENABLED', true),

    /*
    |--------------------------------------------------------------------------
    | Specula server endpoint
    |--------------------------------------------------------------------------
    */
    'endpoint' => env('SPECULA_ENDPOINT', 'http://localhost:7878'),

    /*
    |--------------------------------------------------------------------------
    | Paths to ignore (prefix match, leading slash optional)
    |--------------------------------------------------------------------------
    */
    'ignore' => [
        '/_debugbar',
        '/telescope',
        '/horizon',
        '/health',
        '/metrics',
        '/sanctum/csrf-cookie',
        '/livewire',
    ],

    /*
    |--------------------------------------------------------------------------
    | Webhook path fragments to ignore (substring match)
    |--------------------------------------------------------------------------
    | Any path containing one of these strings is silently skipped.
    | Webhooks use external signatures and are not user-facing endpoints.
    */
    'webhook_prefixes' => [
        'webhook',
        'webhooks',
        'stripe',
        'spreedly',
        'mailgun',
        'onesignal',
    ],

    /*
    |--------------------------------------------------------------------------
    | Capture request / response bodies
    |--------------------------------------------------------------------------
    | Disable if you have strict data privacy requirements.
    | multipart/form-data file fields are always skipped regardless.
    */
    'capture_bodies' => env('SPECULA_CAPTURE_BODIES', true),

    /*
    |--------------------------------------------------------------------------
    | Maximum body size to capture (bytes)
    |--------------------------------------------------------------------------
    | Bodies larger than this are silently skipped to protect memory.
    | Default: 256 KB.
    */
    'max_body_bytes' => env('SPECULA_MAX_BODY_BYTES', 262144),

    /*
    |--------------------------------------------------------------------------
    | Headers to scrub from observations
    |--------------------------------------------------------------------------
    | These headers are never forwarded to the Specula server.
    */
    'scrub_headers' => [
        'authorization',
        'cookie',
        'x-api-key',
        'x-auth-token',
    ],

    /*
    |--------------------------------------------------------------------------
    | Debug mode
    |--------------------------------------------------------------------------
    | When enabled, Specula logs every decision (TRACK / SKIP / WARN) to
    | Laravel's single log channel. Use this to diagnose missing endpoints.
    |
    | Enable: SPECULA_DEBUG=true in .env
    | Then:   tail -f storage/logs/laravel.log | grep Specula
    */
    'debug' => env('SPECULA_DEBUG', false),
];
