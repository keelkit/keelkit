# testdata

Файлы для тестов модуля и для `make images-demo`. Все, кроме `photo.heic`, создаёт
`TestGenerateTestdata` в `testdata_gen_test.go`:

```sh
cd modules/images && go test -run TestGenerateTestdata -gen .
```

| Файл | Что проверяет |
|---|---|
| `orientation-1.jpg` … `orientation-8.jpg` | все 8 значений EXIF Orientation; после поворота — четверти «красный, зелёный / синий, жёлтый», 640×480 |
| `orientation-6.webp` | ориентация из чанка EXIF расширенного WebP (VP8X) |
| `transparent.png` | прозрачность: WebP сохраняет альфа-канал, JPEG — на белом фоне |
| `small.jpg` | 300 px: вариантов шире оригинала нет |
| `photo.jpg` | 2600 px: все ширины профиля 480/960/1600/2400 |
| `bomb.png` | 50000×50000 в заголовке при размере файла 57 байт — отказ до декодирования |
| `photo.heic` | настоящий HEIC — отказ с понятной причиной; сделан на macOS: `sips -s format heic small.jpg --out photo.heic` |

Ориентации построены по таблице из определения тега, а не через код модуля; проверено независимо:
`magick orientation-N.jpg -auto-orient` даёт одну и ту же картинку для всех N.
