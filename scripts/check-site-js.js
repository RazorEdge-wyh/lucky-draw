// 精确校验静态页内联脚本：先剥离注释与字符串字面量，再统计括号，
// 否则注释里的括号会造成假报警。有 Node 时用这个（更准确）。
//
// 用法：node scripts/check-site-js.js
const fs = require("fs");
const path = require("path");

const target = path.join(__dirname, "..", "site", "index.html");
const html = fs.readFileSync(target, "utf8");

const m = html.match(/<script>([\s\S]*?)<\/script>/);
if (!m) {
  console.error("未在 site/index.html 中找到内联 <script> 块");
  process.exit(1);
}
const raw = m[1];

// 剥离 // 行注释、/* */ 块注释、'...' 与 "..." 字符串，避免括号计数被干扰。
function stripNoise(src) {
  let out = "";
  let i = 0;
  while (i < src.length) {
    const two = src.slice(i, i + 2);
    if (two === "//") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if (two === "/*") {
      const end = src.indexOf("*/", i + 2);
      i = end < 0 ? src.length : end + 2;
      continue;
    }
    const ch = src[i];
    if (ch === '"' || ch === "'" || ch === "`") {
      const quote = ch;
      i++;
      while (i < src.length) {
        if (src[i] === "\\") { i += 2; continue; }
        if (src[i] === quote) { i++; break; }
        i++;
      }
      out += '""';
      continue;
    }
    out += ch;
    i++;
  }
  return out;
}

const code = stripNoise(raw);

function count(s, ch) {
  return (s.match(new RegExp("\\" + ch, "g")) || []).length;
}

let ok = true;
for (const [a, b] of [["{", "}"], ["(", ")"], ["[", "]"]]) {
  const na = count(code, a);
  const nb = count(code, b);
  const balanced = na === nb;
  if (!balanced) ok = false;
  console.log(`${a} ${na}  ${b} ${nb}  ${balanced ? "平衡" : "不平衡 <-- 括号数不匹配"}`);
}

// 残留检查：只在剥离后的代码里找，注释里提到不算。
const forbidden = ["specialNumber", "specialProb", "isAccepted", "fallback", "maxAttempts"];
const found = forbidden.filter((f) => code.includes(f));
if (found.length) {
  ok = false;
  console.log("残留有偏算法痕迹：" + found.join(", "));
} else {
  console.log("无有偏算法残留");
}

// 真正的语法解析（只编译，不执行）
try {
  new Function(raw);
  console.log("语法解析：通过");
} catch (e) {
  ok = false;
  console.log("语法解析：失败 -> " + e.message);
}

process.exit(ok ? 0 : 1);
