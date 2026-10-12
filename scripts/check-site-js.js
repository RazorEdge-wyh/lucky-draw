// 校验 site/index.html 里内联 <script> 的括号配平与语法。
//
// 用途：这个静态页把 JS 内联在 HTML 里，没有构建步骤，因此语法错误不会被
// 任何工具自动发现（CI 只跑 Go 测试）。有 Node 时跑一下这个脚本可以提前拦住。
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
const js = m[1];

function count(s, ch) {
  return (s.match(new RegExp("\\" + ch, "g")) || []).length;
}

let ok = true;
for (const [a, b] of [["{", "}"], ["(", ")"], ["[", "]"]]) {
  const na = count(js, a);
  const nb = count(js, b);
  const balanced = na === nb;
  if (!balanced) ok = false;
  console.log(`${a} ${na}  ${b} ${nb}  ${balanced ? "平衡" : "不平衡 <-- 括号数不匹配"}`);
}

// 用 new Function 做真正的语法解析（只编译，不执行）
try {
  new Function(js);
  console.log("语法解析：通过");
} catch (e) {
  ok = false;
  console.log("语法解析：失败 -> " + e.message);
}

console.log("内联脚本长度：" + js.length + " 字符");
process.exit(ok ? 0 : 1);
