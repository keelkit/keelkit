// live-preview.js — живой предпросмотр в редакторе записи (admin.Live в internal/admin/views.templ).
// Форма с задержкой отправляется на data-live-url, ответ — страница сайта — показывается в iframe. Первый ответ
// грузится целиком (srcdoc), следующие заменяют только <main>: прокрутка не сбрасывается, картинки не мигают.
// iframe того же происхождения, поэтому связь блоков — прямым доступом к его документу: щелчок по блоку
// в предпросмотре открывает форму блока, щелчок по заголовку блока в форме прокручивает предпросмотр к нему.
// Ссылки в предпросмотре не открываются, баннера cookies в нём нет. Без этого файла панели не видно, форма работает как обычно.
(function () {
  "use strict";

  var root = document.querySelector(".live[data-live-url]");
  if (!root || !window.fetch) return;

  var url = root.getAttribute("data-live-url");
  var formID = root.getAttribute("data-live-form");
  var pane = root.querySelector(".live-pane");
  var gutter = root.querySelector(".live-gutter");
  var tabs = root.querySelector(".live-tabs");
  var stage = root.querySelector(".live-stage");
  var frame = root.querySelector(".live-frame");
  var zoom = root.querySelector(".live-zoom");
  var status = root.querySelector(".live-status");

  var delay = 500; // мс после последнего ввода
  var timer = null;
  var ctrl = null; // запрос в работе: новый отменяет его
  var loaded = false; // в iframe уже страница — можно заменять <main>

  // Настройки панели — только удобство одного браузера: без localStorage всё работает по умолчанию.
  function load(key) {
    try {
      return window.localStorage.getItem("live-preview:" + key);
    } catch (e) {
      return null;
    }
  }
  function store(key, value) {
    try {
      window.localStorage.setItem("live-preview:" + key, value);
    } catch (e) {
      // приватный режим, запрет хранилища
    }
  }

  function form() {
    return document.getElementById(formID);
  }

  // --- обновление ---

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, delay);
  }

  // refresh отправляет форму как обычный POST (urlencoded, с CSRF-полем формы). full — загрузить страницу заново.
  function refresh(full) {
    clearTimeout(timer);
    var f = form();
    if (!f) return;
    if (ctrl) ctrl.abort();
    var mine = (ctrl = new AbortController());
    status.textContent = "Обновляется…";
    fetch(url, {
      method: "POST",
      body: new URLSearchParams(new FormData(f)),
      credentials: "same-origin",
      signal: mine.signal,
    })
      .then(function (resp) {
        if (!resp.ok) throw new Error("HTTP " + resp.status);
        return resp.text();
      })
      .then(function (html) {
        if (mine !== ctrl) return;
        ctrl = null;
        show(html, full === true);
        status.textContent = "";
      })
      .catch(function (err) {
        if (err.name === "AbortError") return;
        ctrl = null;
        status.textContent = "Не удалось обновить";
      });
  }

  function show(html, full) {
    var doc = loaded && frame.contentDocument;
    var main = doc && doc.querySelector("main");
    var next = !full && main && new DOMParser().parseFromString(html, "text/html");
    var nextMain = next && next.querySelector("main");
    if (!nextMain) {
      var win = frame.contentWindow;
      scrollY = loaded && win ? win.scrollY : 0;
      loaded = false;
      frame.srcdoc = html;
      return;
    }
    main.replaceWith(doc.importNode(nextMain, true));
    doc.title = next.title;
  }

  var scrollY = 0; // прокрутка, которую вернуть после полной загрузки

  frame.addEventListener("load", function () {
    var doc = frame.contentDocument;
    if (!doc || !doc.querySelector("main")) return;
    loaded = true;
    // Баннер cookies — не часть записи: в предпросмотре он только закрывал бы низ страницы.
    var banner = doc.querySelector(".cookie-banner");
    if (banner) banner.remove();
    frame.contentWindow.scrollTo(0, scrollY);
    doc.addEventListener("click", onFrameClick, true);
  });

  function inForm(el) {
    var f = form();
    return f && el && f.contains(el);
  }

  document.addEventListener("input", function (ev) {
    if (inForm(ev.target)) schedule();
  });
  document.addEventListener("change", function (ev) {
    if (inForm(ev.target)) schedule();
  });
  // Кнопки блоков (↑ ↓, «Скрыть», «Добавить блок», выбор картинки) перерисовывают форму через htmx.
  document.body.addEventListener("htmx:afterSettle", function (ev) {
    var t = ev.detail && ev.detail.target;
    if (t && (t.id === formID || inForm(t))) schedule();
  });

  // --- связь блоков ---

  function flash(el) {
    el.classList.remove("live-flash");
    void el.offsetWidth; // перезапустить анимацию при повторном щелчке
    el.classList.add("live-flash");
  }

  // Щелчок в предпросмотре: ссылки не уводят из редактора, блок (id="block-<id>") открывает свою форму.
  function onFrameClick(ev) {
    var t = ev.target;
    if (!t.closest) return;
    if (t.closest("a[href]")) ev.preventDefault();
    var block = t.closest(".block[id^='block-']");
    var sec = block && document.getElementById(block.id);
    if (!sec || !inForm(sec)) return;
    setTab("editor");
    sec.scrollIntoView({ behavior: "smooth", block: "start" });
    flash(sec);
  }

  // Щелчок по заголовку блока в форме прокручивает предпросмотр к блоку и подсвечивает его.
  document.addEventListener("click", function (ev) {
    var h = ev.target.closest && ev.target.closest(".block-form > .block-head h3");
    var sec = h && h.closest(".block-form");
    if (!sec || !inForm(sec) || !loaded) return;
    var el = frame.contentDocument.getElementById(sec.id);
    if (!el) return;
    setTab("preview");
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    el.style.outline = "3px solid #0f766e";
    el.style.outlineOffset = "4px";
    setTimeout(function () {
      el.style.outline = "";
      el.style.outlineOffset = "";
    }, 1200);
  });

  // --- ширина экрана ---

  var width = parseInt(load("width"), 10) || 0;
  var widthButtons = root.querySelectorAll("[data-live-width]");

  function setWidth(w) {
    var found = false;
    widthButtons.forEach(function (b) {
      var on = parseInt(b.getAttribute("data-live-width"), 10) === w;
      b.setAttribute("aria-pressed", on ? "true" : "false");
      found = found || on;
    });
    if (!found) return setWidth(parseInt(widthButtons[0].getAttribute("data-live-width"), 10));
    width = w;
    store("width", String(w));
    fit();
  }

  // fit рисует iframe в выбранной ширине и уменьшает его под панель: видна настоящая вёрстка этой ширины.
  function fit() {
    var w = stage.clientWidth, h = stage.clientHeight;
    if (!w || !h) return;
    var s = Math.min(1, w / width);
    frame.style.width = width + "px";
    frame.style.height = h / s + "px";
    frame.style.transform = "scale(" + s + ")";
    frame.style.left = Math.max(0, (w - width * s) / 2) + "px";
    zoom.textContent = width + " px · " + Math.round(s * 100) + "%";
  }

  widthButtons.forEach(function (b) {
    b.addEventListener("click", function () {
      setWidth(parseInt(b.getAttribute("data-live-width"), 10));
    });
  });
  if (window.ResizeObserver) new ResizeObserver(fit).observe(stage);
  else window.addEventListener("resize", fit);

  // --- граница панелей ---

  var minEditor = 320, minPane = 360;

  function setEditorWidth(px) {
    var max = root.clientWidth - gutter.offsetWidth - minPane;
    px = Math.round(Math.max(minEditor, Math.min(px, max)));
    root.style.setProperty("--live-editor-w", px + "px");
    gutter.setAttribute("aria-valuenow", String(px));
    store("editor", String(px));
  }

  gutter.addEventListener("pointerdown", function (ev) {
    ev.preventDefault();
    gutter.setPointerCapture(ev.pointerId);
    root.classList.add("is-resizing");
    var left = root.getBoundingClientRect().left;
    function move(e) {
      setEditorWidth(e.clientX - left);
    }
    function up() {
      root.classList.remove("is-resizing");
      gutter.removeEventListener("pointermove", move);
      gutter.removeEventListener("pointerup", up);
      gutter.removeEventListener("pointercancel", up);
    }
    gutter.addEventListener("pointermove", move);
    gutter.addEventListener("pointerup", up);
    gutter.addEventListener("pointercancel", up);
  });

  gutter.addEventListener("keydown", function (ev) {
    var step = ev.shiftKey ? 64 : 16;
    var cur = root.querySelector(".live-editor").offsetWidth;
    if (ev.key === "ArrowLeft") setEditorWidth(cur - step);
    else if (ev.key === "ArrowRight") setEditorWidth(cur + step);
    else return;
    ev.preventDefault();
  });

  // --- вкладки на узком экране ---

  var tabButtons = tabs.querySelectorAll("[data-live-tab]");

  function setTab(name) {
    root.classList.toggle("show-preview", name === "preview");
    tabButtons.forEach(function (b) {
      b.setAttribute("aria-pressed", b.getAttribute("data-live-tab") === name ? "true" : "false");
    });
  }

  tabButtons.forEach(function (b) {
    b.addEventListener("click", function () {
      setTab(b.getAttribute("data-live-tab"));
    });
  });

  root.querySelector(".live-reload").addEventListener("click", function () {
    refresh(true);
  });

  // --- включение ---

  pane.hidden = false;
  gutter.hidden = false;
  tabs.hidden = false;
  root.classList.add("is-on");
  var saved = parseInt(load("editor"), 10);
  if (saved) setEditorWidth(saved);
  setWidth(width);
  refresh(true);
})();
