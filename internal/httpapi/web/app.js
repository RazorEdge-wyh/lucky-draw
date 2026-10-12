/* 极简抽取前端：点一次，抽一个数字。九次为一轮。 */
(function () {
  "use strict";

  var numEl = document.getElementById("number");
  var btn = document.getElementById("draw");
  var progressEl = document.getElementById("progress");
  var resultEl = document.getElementById("result");

  var TOTAL = 9;      // 一轮九次，与后端 DRAW_COUNT 一致；首屏会用响应里的 total 校正
  var SPECIAL = 4;    // 特殊号码，仅用于抽满后的小结统计
  var poolSize = TOTAL; // 号码池大小，仅用于滚动动画里生成临时数字
  var busy = false;
  var round = 0;      // 当前轮数
  var index = 0;      // 本轮已抽次数
  var items = [];     // 本轮已抽出的号码，用于「本轮结果」展示

  /* -------------------------------------------------------------- 本轮结果 */

  // 把本轮已抽的号码按顺序排开，统一样式，不区分哪个是特殊号码；
  // 还没抽的位置用虚线占位，让人一眼看出本轮还剩几次。
  function renderResult(justFinished) {
    resultEl.innerHTML = "";

    if (items.length === 0) {
      var tip = document.createElement("span");
      tip.className = "empty";
      tip.textContent = "点上方按钮开始，本轮结果会显示在这里";
      resultEl.appendChild(tip);
      return;
    }

    for (var i = 0; i < TOTAL; i++) {
      var chip = document.createElement("i");
      if (i < items.length) {
        chip.textContent = String(items[i]);
        chip.title = "第 " + (i + 1) + " 次";
      } else {
        chip.className = "placeholder";
        chip.textContent = "·";
      }
      resultEl.appendChild(chip);
    }

    // 抽满才给小结：这一轮 4 出现了几次、其他号码最多几次
    if (justFinished) {
      var counts = {};
      for (var j = 0; j < items.length; j++) {
        counts[items[j]] = (counts[items[j]] || 0) + 1;
      }
      var specialCount = counts[SPECIAL] || 0;
      var otherMax = 0;
      Object.keys(counts).forEach(function (k) {
        var n = Number(k);
        if (n !== SPECIAL && counts[k] > otherMax) { otherMax = counts[k]; }
      });

      var sum = document.createElement("div");
      sum.className = "summary";
      sum.innerHTML = "本轮 " + items.length + " 次 · " +
        SPECIAL + " 出现 <b>" + specialCount + "</b> 次 · 其他号码最多 " + otherMax + " 次";
      resultEl.appendChild(sum);
    }
  }

  /* ---------------------------------------------------------------- 进度点 */

  function renderProgress() {
    if (progressEl.childElementCount !== TOTAL) {
      progressEl.innerHTML = "";
      for (var i = 0; i < TOTAL; i++) {
        progressEl.appendChild(document.createElement("i"));
      }
    }
    for (var j = 0; j < TOTAL; j++) {
      progressEl.children[j].className = j < index ? "done" : "";
    }
  }

  /* ---------------------------------------------------------------- 动画 */

  // 滚动动画：号码先快速跳变，减速到停，最后定格在服务端返回的真实结果上。
  // 真实结果在动画开始前就已经拿到，动画只负责表现，不参与决定。
  function rollTo(finalNumber, onDone) {
    var el = numEl;
    var elapsed = 0;
    var DURATION = 620;
    var START_TICK = 45;   // 起始跳变间隔（毫秒）
    var END_TICK = 190;    // 结束跳变间隔
    var poolSize = TOTAL;  // 号码池大小，用于生成跳变期间的临时数字

    el.className = "rolling";

    function tick() {
      var t = elapsed / DURATION;
      // 用平方缓动：前段跳得快，后段明显变慢
      var gap = START_TICK + (END_TICK - START_TICK) * t * t;

      // 临时数字只用于视觉，范围 1..poolSize 与真实号码池接近即可
      el.textContent = String(1 + Math.floor(Math.random() * poolSize));

      if (elapsed >= DURATION) {
        el.textContent = String(finalNumber);
        el.className = "settle";
        // 重放动画：先清空 class 再强制重排，这样每次定格都会重新弹一下
        void el.offsetWidth;
        el.className = "settle";
        if (onDone) { onDone(); }
        return;
      }
      elapsed += gap;
      setTimeout(tick, gap);
    }
    tick();
  }

  /* ---------------------------------------------------------------- 抽取 */

  function fail(msg) {
    busy = false;
    btn.disabled = false;
    btn.textContent = "抽 取";
    numEl.className = "";
    numEl.textContent = "—";
    // 出错时用最朴素的方式告诉用户，不引入额外的 UI 元素
    console.error(msg);
    alert(msg);
  }
  function draw() {
    if (busy) { return; }
    busy = true;
    btn.disabled = true;
    btn.textContent = "抽取中…";

    // 禁用缓存，避免某些浏览器把 GET 结果缓存住导致抽到同一个数
    fetch("/api/draw?t=" + Date.now(), {
      headers: { Accept: "application/json" },
      cache: "no-store"
    })
      .then(function (r) {
        return r.json().then(function (body) {
          if (!r.ok) { throw new Error(body && body.error ? body.error : "HTTP " + r.status); }
          return body;
        });
      })
      .then(function (res) {
        TOTAL = res.total || TOTAL;
        round = res.round;
        index = res.index;
        poolSize = TOTAL;

        // 编号从 1 开始，因此第 1 次抽取时把上一轮的结果清掉
        if (res.index === 1) { items = []; }
        items.push(res.number);

        rollTo(res.number, function () {
          renderProgress();
          renderResult(res.roundDone);

          // 一轮抽满后，进度点与本轮结果一起归零，自动进入下一轮
          if (res.roundDone) {
            setTimeout(function () {
              index = 0;
              items = [];
              renderProgress();
              renderResult(false);
            }, 2200);
          }
          busy = false;
          btn.disabled = false;
          btn.textContent = "抽 取";
        });
      })
      .catch(function (e) {
        fail("抽取失败：" + e.message);
      });
  }

  btn.addEventListener("click", draw);

  // 空格键也能抽，符合工具类网站的使用习惯
  document.addEventListener("keydown", function (e) {
    if (e.code === "Space" && document.activeElement !== btn) {
      e.preventDefault();
      draw();
    }
  });

  renderProgress();
  renderResult(false);
})();
