// Рендер DOT → SVG через Viz.js (Graphviz/WASM).
// У Graphviz в Viz.js нет метрик кириллицы: ширина такого символа считается
// как у пробела, и подписи вылезают из блоков. Поэтому раскладка строится по
// копии исходника, где каждый не-ASCII символ заменён ASCII-символом близкой
// ширины (замена 1:1, длина строк сохраняется), а в готовом SVG текст
// возвращается из оригинала по тем же позициям.
// Запуск: node render.mjs <путь к viz-standalone.mjs> *.dot
import fs from "node:fs";
import { pathToFileURL } from "node:url";
const [vizPath, ...files] = process.argv.slice(2);
const { instance } = await import(pathToFileURL(vizPath));
const viz = await instance();

const wide = new Set([..."мшщжюыМШЩЖЮЫ—→≤≥"]);
const narrow = new Set([..."гтзсэкчьъ·"]);
function sub(ch) {
  if (ch.charCodeAt(0) < 128) return ch;
  if (wide.has(ch)) return /[А-ЯЁ]/.test(ch) ? "M" : "m";
  if (narrow.has(ch)) return ch === "·" ? "." : "c";
  if (/[А-ЯЁ]/.test(ch)) return "H";
  if (ch === "«" || ch === "»") return "o";
  return "o";
}
const translit = (s) => [...s].map(sub).join("");
const unesc = (s) => s.replace(/&#(\d+);/g, (m, n) => String.fromCharCode(+n)).replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, "&");
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

for (const f of files) {
  const src = fs.readFileSync(f, "utf8");
  const tsrc = translit(src);
  if (tsrc.length !== src.length) throw new Error(f + ": замена изменила длину");
  // Тексты в исходнике бывают в двух видах: как есть и с HTML-сущностями.
  const srcDecoded = unesc(src), tDecoded = translit(srcDecoded);
  const r = viz.render(tsrc, { format: "svg" });
  for (const e of r.errors) console.error(f, e.level, e.message);
  if (r.status !== "success") { console.log(f, r.status); continue; }
  let miss = 0;
  const out = r.output.replace(/(<text\b[^>]*>)([^<]*)(<\/text>)/g, (m, a, t, b) => {
    const plain = unesc(t);
    if (!/[A-Za-z]/.test(plain)) return m;
    // Подпись в исходнике ограничена кавычкой, тегом или \n с обеих сторон:
    // без этого короткое слово находится внутри длинного.
    const before = (k) => k === 0 || '">'.includes(srcDecoded[k - 1]) || srcDecoded.slice(k - 2, k) === "\\n";
    const after = (k) => '"<\\'.includes(srcDecoded[k] ?? '"');
    for (let i = tDecoded.indexOf(plain); i >= 0; i = tDecoded.indexOf(plain, i + 1)) {
      if (before(i) && after(i + plain.length)) return a + esc(srcDecoded.slice(i, i + plain.length)) + b;
    }
    miss++; console.error(f, "не найден текст:", plain); return m;
  }).replace(/<title>[^<]*<\/title>/g, "");
  fs.writeFileSync(f.replace(/\.dot$/, ".svg"), out);
  console.log(f, "ok", miss ? `(${miss} без замены)` : "");
}
