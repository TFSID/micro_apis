# Micro API

Starter project untuk REST API serverless menggunakan Go, Huma v2, Chi, AWS Lambda custom runtime, dan API Gateway HTTP API.

## Prasyarat

- Go 1.25 atau lebih baru
- Node.js dan npm (untuk Serverless Framework v4 melalui `npx`)
- AWS CLI dengan kredensial dan region yang sudah dikonfigurasi
- Autentikasi Serverless Framework v4

## Menjalankan secara lokal

```powershell
go run ./cmd/api
```

Server berjalan di port `8888` secara default; ubah dengan environment variable `PORT`. Endpoint awal:

- `GET /health` — pemeriksaan kesehatan
- `GET /greeting/{name}` — contoh path parameter dengan validasi Huma
- `/docs` — dokumentasi interaktif OpenAPI
- `/openapi.json` — spesifikasi OpenAPI

Jalankan test dengan `go test ./...`.

## Konversi Nuclei ke cURL

Untuk alur sederhana dengan URL template saja, gunakan `POST /convert/url` dengan body JSON berisi satu field `url`. Contoh memakai template yang diberikan:

```json
{
	"url": "https://raw.githubusercontent.com/projectdiscovery/nuclei-templates/refs/heads/main/http/cves/2026/CVE-2026-1340.yaml"
}
```

Endpoint menghasilkan command dengan target placeholder `https://TARGET`; ganti placeholder tersebut dengan target yang ingin diuji sebelum menjalankan command. URL pada input adalah lokasi file template, bukan target scan.

`POST /convert` menerima tepat satu sumber template: YAML langsung (`template_yaml`), URL publik (`template_url`), path relatif di repo resmi (`template_path`), atau nama file root sederhana (`template_id`). Request path relatif memerlukan `base_url`; nilai placeholder konkret dapat diberikan melalui `variables`.

Contoh:

```json
{
	"template_yaml": "id: sample\nhttp:\n  - method: GET\n    path:\n      - '{{BaseURL}}/health/{{version}}'\n",
	"base_url": "https://target.example",
	"variables": { "version": "v1" }
}
```

Respons berisi `commands`, setiap command mencakup `method`, `url`, daftar argumen terstruktur (`args`), dan string shell POSIX (`command`). Beberapa path atau request menghasilkan beberapa command. API tidak menjalankan cURL.

Parser awal mendukung request HTTP terstruktur (`method`, `path`, `headers`, `body`), HTTP `raw` sederhana, variabel konkret, dan opsi redirect dasar. Payload generator, unsafe/pipeline/race/cookie-reuse, non-HTTP protocol, workflow, fuzzing, matcher/extractor evaluation, dan chaining tidak didukung; fitur tidak statis ditolak atau dicantumkan sebagai warning, bukan disimulasikan. Ukuran template dibatasi 1 MiB dan hasil maksimal 20 request.

Pengambilan URL template memakai HTTP(S), membatasi port 80/443, waktu dan ukuran respons, menolak alamat lokal/private/reserved, menonaktifkan proxy environment, dan tidak mengikuti redirect. `template_path` hanya relatif di bawah `templates/` repo ProjectDiscovery. `template_id` saat ini hanya mengasumsikan file dengan nama tersebut berada langsung di root `templates/`; untuk template di kategori/subfolder gunakan `template_path` atau `template_url`. Integrasi ini hanya mengambil dokumen template; target dalam template tidak di-fetch atau di-scan.

Sebelum endpoint dipublikasikan, tambahkan authorizer dan rate limit sesuai kebutuhan. Perlindungan SSRF pada fetch template tidak menggantikan kontrol akses layanan.

## Build dan deploy ke AWS

Konfigurasi memakai runtime `provided.al2023`, arsitektur ARM64, dan API Gateway HTTP API payload v2. Script PowerShell membangun binary Lambda bernama `bootstrap`, lalu mengemasnya ke `.build/micro-api.zip` dengan permission executable yang diperlukan Lambda:

```powershell
.\scripts\build.ps1
npx serverless deploy --stage dev --region ap-southeast-1
```

Atau di lingkungan Linux/macOS yang memiliki GNU Make, `zip`, dan `make`:

```sh
make test
make build
make deploy
```

Ganti stage dan region sesuai environment AWS. Pastikan kredensial AWS dan autentikasi Serverless Framework v4 siap sebelum deploy. Serverless mengeluarkan URL API setelah deployment berhasil.

## Struktur penting

- `cmd/api` — entry point lokal dan Lambda
- `internal/api` — router Chi dan operasi Huma
- `internal/lambdahttp` — adapter API Gateway HTTP API v2 ke `net/http`
- `serverless.yml` — definisi AWS Lambda dan API Gateway
