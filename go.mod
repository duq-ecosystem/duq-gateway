module duq-gateway

go 1.25.7

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/caddyserver/certmagic v0.21.7
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.12.3
	github.com/redis/go-redis/v9 v9.18.0
	github.com/yuin/goldmark v1.8.2
	golang.org/x/crypto v0.49.0
	duq-tracing v0.0.0
)

require (
	github.com/caddyserver/zerossl v0.1.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/libdns/libdns v0.2.2 // indirect
	github.com/mholt/acmez/v3 v3.0.1 // indirect
	github.com/miekg/dns v1.1.62 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
)

replace duq-tracing => ../duq-tracing/go
