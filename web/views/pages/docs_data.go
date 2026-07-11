package pages

// APIEndpoint describes one operation for the docs page.
type APIEndpoint struct {
	Method      string // GET, POST, PATCH, DELETE
	Path        string // e.g. /buckets
	Summary     string // short description
	Description string // longer text (optional)
	Example     string // curl example (optional)
}

// APIGroup bundles related endpoints.
type APIGroup struct {
	Title     string
	Subtitle  string
	Endpoints []APIEndpoint
}

// apiGroups holds the documented operations. Kept as data (rather than parsed
// from openapi.yaml) so we can ship curated curl examples without pulling in a
// YAML dependency; the raw spec remains available at /docs/openapi.yaml.
func apiGroups() []APIGroup {
	return []APIGroup{
		{
			Title:    "Аутентификация",
			Subtitle: "Все запросы к API выполняются с API-ключом в заголовке Authorization.",
			Endpoints: []APIEndpoint{
				{
					Method:      "GET",
					Path:        "/api-keys",
					Summary:     "Список API-ключей",
					Description: "Возвращает все ключи аккаунта. Secret показывается только один раз при создании.",
				},
				{
					Method:  "POST",
					Path:    "/api-keys",
					Summary: "Создать API-ключ",
					Example: "curl -X POST https://pulsar.example.com/api/v1/api-keys \\\n  -H \"Authorization: Bearer pk_live_...\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"name\":\"CI backups\",\"scopes\":[\"buckets:write\"]}'",
				},
				{
					Method:  "DELETE",
					Path:    "/api-keys/{id}",
					Summary: "Отозвать ключ",
				},
			},
		},
		{
			Title:    "Бакеты",
			Subtitle: "Контейнеры для файлов. Имя должно соответствовать DNS-нотации (3–63 символа).",
			Endpoints: []APIEndpoint{
				{
					Method:  "GET",
					Path:    "/buckets",
					Summary: "Список бакетов",
					Example: "curl https://pulsar.example.com/api/v1/buckets \\\n  -H \"Authorization: Bearer pk_live_...\"",
				},
				{
					Method:  "POST",
					Path:    "/buckets",
					Summary: "Создать бакет",
					Example: "curl -X POST https://pulsar.example.com/api/v1/buckets \\\n  -H \"Authorization: Bearer pk_live_...\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"name\":\"my-bucket\",\"region\":\"us-east-1\",\"visibility\":\"private\"}'",
				},
				{
					Method:  "GET",
					Path:    "/buckets/{id}",
					Summary: "Получить бакет",
				},
				{
					Method:  "PATCH",
					Path:    "/buckets/{id}",
					Summary: "Обновить настройки (visibility, cdn_enabled)",
				},
				{
					Method:  "DELETE",
					Path:    "/buckets/{id}",
					Summary: "Удалить бакет и все объекты внутри",
				},
			},
		},
		{
			Title:    "Объекты и загрузка",
			Subtitle: "Файлы загружаются напрямую в хранилище по presigned-ссылке в три шага.",
			Endpoints: []APIEndpoint{
				{
					Method:  "GET",
					Path:    "/buckets/{id}/objects",
					Summary: "Список объектов (параметр ?prefix= фильтрует по префиксу)",
				},
				{
					Method:      "POST",
					Path:        "/buckets/{id}/objects/presign-upload",
					Summary:     "Получить одноразовую PUT-ссылку",
					Description: "Шаг 1: запросите подписанный URL. Ответ содержит {\"url\": ..., \"method\": \"PUT\"}.",
					Example:     "curl -X POST https://pulsar.example.com/api/v1/buckets/{id}/objects/presign-upload \\\n  -H \"Authorization: Bearer pk_live_...\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"key\":\"report.pdf\",\"content_type\":\"application/pdf\",\"size\":1048576}'",
				},
				{
					Method:      "PUT",
					Path:        "{presigned url}",
					Summary:     "Загрузить файл напрямую в S3",
					Description: "Шаг 2: отправьте тело файла PUT-запросом по ссылке из предыдущего шага. Без заголовка Authorization.",
					Example:     "curl -X PUT \"https://...presigned-url...\" \\\n  --upload-file report.pdf",
				},
				{
					Method:      "POST",
					Path:        "/buckets/{id}/objects/confirm",
					Summary:     "Зафиксировать метаданные объекта",
					Description: "Шаг 3: подтвердите загрузку, чтобы объект появился в списке.",
					Example:     "curl -X POST https://pulsar.example.com/api/v1/buckets/{id}/objects/confirm \\\n  -H \"Authorization: Bearer pk_live_...\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"key\":\"report.pdf\",\"size\":1048576,\"content_type\":\"application/pdf\"}'",
				},
				{
					Method:  "GET",
					Path:    "/buckets/{id}/objects/{key}/presign-download",
					Summary: "Получить одноразовую GET-ссылку для скачивания",
				},
				{
					Method:  "DELETE",
					Path:    "/buckets/{id}/objects/{key}",
					Summary: "Удалить объект",
				},
			},
		},
		{
			Title:    "Аккаунт",
			Subtitle: "Профиль и сводка использования.",
			Endpoints: []APIEndpoint{
				{
					Method:  "GET",
					Path:    "/me",
					Summary: "Профиль текущего пользователя",
				},
				{
					Method:  "GET",
					Path:    "/me/usage",
					Summary: "Сводка использования: хранилище, трафик, API-вызовы",
				},
			},
		},
	}
}

// idPlaceholder is the literal "{id}" rendered in docs prose. Defined as a
// constant because templ cannot embed a literal "{" inside a { expr } block.
var idPlaceholder = "{id}"

// problemExample is a sample RFC 9457 error body shown on the docs page. Kept
// as a variable because templ's expression parser chokes on a "{" inside an
// inline string argument to a component call.
var problemExample = `{
  "type": "https://pulsar.local/errors/validation_error",
  "title": "Validation failed",
  "status": 400,
  "code": "validation_error",
  "detail": "bucket name must be 3-63 characters"
}`

// authExample is the curl snippet for the authentication section.
var authExample = "curl https://pulsar.example.com/api/v1/buckets \\\n  -H \"Authorization: Bearer pk_live_a1b2c3d4e5\""

// apiBase returns the full API base URL shown on the docs page. templ cannot
// place literal text directly after a { expr } block, so we compose it here.
func apiBase(baseURL string) string {
	return baseURL + "/api/v1"
}

// methodColor maps an HTTP method to a Tailwind badge color class.
func methodColor(method string) string {
	switch method {
	case "GET":
		return "bg-[#dde7d5] border border-[#3f6b35] text-[#2f5227]"
	case "POST":
		return "bg-[#f0e6d6] border border-[#b06a2c] text-[#8f511e]"
	case "PUT":
		return "bg-[#f5e7cf] border border-[#b87d20] text-[#7a5215]"
	case "PATCH":
		return "bg-[#f3e0d2] border border-[#8f511e] text-[#6e3e16]"
	case "DELETE":
		return "bg-[#f3dcd6] border border-[#a23b29] text-[#7a2b1c]"
	default:
		return "bg-[#f0e6d6] border border-[#9a8770] text-[#3d2a1c]"
	}
}
