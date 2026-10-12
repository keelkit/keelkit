// editor.js — панель оформления Markdown в админке (markdownToolbar в internal/blocks/form.templ).
// Кнопка оборачивает выделение разметкой Markdown, повторное нажатие снимает её; Ctrl/⌘+B, I, K — то же с клавиатуры.
// Без этого файла панели не видно (CSS показывает её только при классе js), textarea работает как обычно.
(function () {
  "use strict";

  document.documentElement.classList.add("js");

  var inline = { bold: "**", italic: "*", strike: "~~", code: "`" };
  var linePatterns = { heading: /^#{1,6} /, ul: /^[-*+] /, ol: /^\d+[.)] /, quote: /^> ?/ };

  // replace заменяет [start, end) на text и выделяет [selStart, selEnd) в новом тексте.
  // insertText сохраняет историю отмены (Ctrl+Z) и сам шлёт input — по нему htmx обновляет превью;
  // где его нет — setRangeText и input вручную.
  function replace(ta, start, end, text, selStart, selEnd) {
    ta.focus();
    ta.setSelectionRange(start, end);
    var ok = false;
    try {
      ok = text !== "" && document.execCommand("insertText", false, text);
    } catch (e) {
      ok = false;
    }
    if (!ok) {
      ta.setRangeText(text, start, end);
      ta.dispatchEvent(new Event("input", { bubbles: true }));
    }
    ta.setSelectionRange(selStart, selEnd);
  }

  // has — есть ли разметка m в серии одинаковых символов длины run: «*» — курсив, если звёзд нечётно
  // (*x*, ***x***), «**» — жирный, если их от двух; остальное — от длины разметки.
  function has(run, m) {
    if (m === "*") return run % 2 === 1;
    if (m === "**") return run >= 2;
    return run >= m.length;
  }

  function runBefore(v, pos, ch) {
    var n = 0;
    while (pos - n > 0 && v[pos - n - 1] === ch) n++;
    return n;
  }

  function runAfter(v, pos, ch) {
    var n = 0;
    while (pos + n < v.length && v[pos + n] === ch) n++;
    return n;
  }

  function wrap(ta, m) {
    var v = ta.value, s = ta.selectionStart, e = ta.selectionEnd, n = m.length, ch = m[0];
    // Пробелы по краям выделения остаются снаружи: «**слово **» Markdown не распознаёт.
    while (s < e && /\s/.test(v[s])) s++;
    while (e > s && /\s/.test(v[e - 1])) e--;
    var sel = v.slice(s, e);

    // Разметка вокруг выделения: **|слово|** → слово.
    if (has(Math.min(runBefore(v, s, ch), runAfter(v, e, ch)), m)) {
      replace(ta, s - n, e + n, sel, s - n, e - n);
      return;
    }
    // Разметка внутри выделения: |**слово**| → слово.
    var left = runAfter(sel, 0, ch), right = runBefore(sel, sel.length, ch);
    if (left < sel.length && has(Math.min(left, right), m)) {
      replace(ta, s, e, sel.slice(n, sel.length - n), s, e - 2 * n);
      return;
    }
    // Без выделения — пустая разметка, курсор внутри.
    replace(ta, s, e, m + sel + m, s + n, e + n);
  }

  // lines ставит или снимает префикс строки (подзаголовок, списки, цитата) у всех строк выделения.
  function lines(ta, kind) {
    var v = ta.value, s = ta.selectionStart, e = ta.selectionEnd;
    if (e > s && v[e - 1] === "\n") e--; // выделение до начала следующей строки её не захватывает
    s = v.lastIndexOf("\n", s - 1) + 1;
    e = v.indexOf("\n", e);
    if (e < 0) e = v.length;

    var re = linePatterns[kind];
    var ls = v.slice(s, e).split("\n");
    var filled = ls.filter(function (l) { return l.trim() !== ""; });
    var remove = filled.length > 0 && filled.every(function (l) { return re.test(l); });
    var num = 0;
    var out = ls.map(function (l) {
      if (l.trim() === "") return l;
      if (remove) return l.replace(re, "");
      // Смена вида: подзаголовок другого уровня, список другого типа — прежний префикс убирается.
      if (kind === "heading") l = l.replace(linePatterns.heading, "");
      if (kind === "ul" || kind === "ol") l = l.replace(/^([-*+]|\d+[.)]) /, "");
      var p = kind === "heading" ? "## " : kind === "ul" ? "- " : kind === "quote" ? "> " : ++num + ". ";
      return p + l;
    }).join("\n");
    replace(ta, s, e, out, s, s + out.length);
  }

  // link: выделенный текст — текст ссылки, выделяется «https://» для ввода адреса; выделенный адрес — адрес ссылки,
  // выделяется «текст».
  function link(ta) {
    var v = ta.value, s = ta.selectionStart, e = ta.selectionEnd, sel = v.slice(s, e);
    if (/^https?:\/\/\S+$/.test(sel)) {
      replace(ta, s, e, "[текст](" + sel + ")", s + 1, s + 6);
      return;
    }
    var text = sel || "текст";
    var url = s + text.length + 3; // после «[текст](»
    if (sel) {
      replace(ta, s, e, "[" + text + "](https://)", url, url + 8);
    } else {
      replace(ta, s, e, "[" + text + "](https://)", s + 1, s + 1 + text.length);
    }
  }

  function apply(ta, kind) {
    if (inline[kind]) wrap(ta, inline[kind]);
    else if (kind === "link") link(ta);
    else if (linePatterns[kind]) lines(ta, kind);
  }

  // Делегирование на document: блоки, добавленные через htmx, работают без повторной инициализации.
  // mousedown не забирает фокус у textarea — выделение и курсор остаются на месте.
  document.addEventListener("mousedown", function (ev) {
    if (ev.target.closest && ev.target.closest(".md-toolbar [data-md]")) ev.preventDefault();
  });

  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest(".md-toolbar [data-md]");
    if (!btn) return;
    var ta = document.getElementById(btn.closest(".md-toolbar").getAttribute("aria-controls"));
    if (ta) apply(ta, btn.getAttribute("data-md"));
  });

  // ev.code, а не ev.key: в русской раскладке Ctrl+B даёт key «и».
  var keys = { KeyB: "bold", KeyI: "italic", KeyK: "link" };
  document.addEventListener("keydown", function (ev) {
    if (!(ev.ctrlKey || ev.metaKey) || ev.altKey || ev.shiftKey || !keys[ev.code]) return;
    var ta = ev.target;
    if (!ta.matches || !ta.matches(".md-field textarea")) return;
    ev.preventDefault();
    apply(ta, keys[ev.code]);
  });
})();
