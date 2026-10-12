# UPGRADE — spec

Инструкции для агентов: что менять в `site.yaml` сайта при переходе на версию валидатора (`SPEC_VERSION` в `Makefile` сайта).

## v0.3.0

**Что изменилось.** Имена полей `seo_title`, `seo_description`, `seo_image` служебные: SEO записи вручную
описывается feature `seo`, а не полями.

**Что сделать в `site.yaml`.** У каждого типа, где описаны поля `seo_*`, удалить эти три поля из `fields`
и добавить `seo` в `features`:

```yaml
# было
pages:
  fields:
    slug:            { type: slug, required: true }
    seo_title:       { type: string }
    seo_description: { type: text }
    seo_image:       { type: image }
  features: [draft, sitemap]
# стало
pages:
  fields:
    slug: { type: slug, required: true }
  features: [draft, sitemap, seo]
```

Код сайта, БД и миграции не меняются: колонки `seo_*` остаются как есть. Тест сайта, сверявший поля `seo_*`
с кодом, сверяет теперь типы с feature `seo` (в эталоне — `seoTypes` в `cmd/site/spec_test.go`).

**Проверка.** `make spec-validate` сайта с `SPEC_VERSION=v0.3.0` проходит; тесты сайта проходят.
Повторная обработка данных не нужна.
