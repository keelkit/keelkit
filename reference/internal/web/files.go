package web

import (
	"strconv"
	"strings"

	"github.com/voffkin/keelkit/modules/files"
)

// FilesPrefix — путь, под которым смонтирован files.Service.Handler (newMux в cmd/site).
const FilesPrefix = "/files/"

// FileURL — адрес файла на сайте. Не меняется при замене файла в админке.
func FileURL(f *files.File) string { return FilesPrefix + f.Path }

// FileFormat — формат файла для людей: «PDF», «MP4», «WebM», «SVG».
func FileFormat(f files.Format) string {
	if f == files.WebM {
		return "WebM"
	}
	return strings.ToUpper(string(f))
}

// FormatBytes — размер файла: КБ (с округлением вверх) или МБ с одним знаком после запятой.
func FormatBytes(n int64) string {
	if n < 1<<20 {
		return strconv.FormatInt((n+1023)>>10, 10) + " КБ"
	}
	return strings.Replace(strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64), ".", ",", 1) + " МБ"
}
