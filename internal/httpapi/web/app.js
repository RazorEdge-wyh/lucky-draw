/* 抽奖控制台前端逻辑：无框架、无外部依赖，纯 DOM 操作。 */
(function () {
  "use strict";

  var $ = function (id) { return document.getElementById(id); };

  var state = {
    meta: null,
    scenario: "challenging",
    prob: 15,        // 百分比，仅在前端保存，随请求下发
    drawCount: 9,
    maxNumber: 8,
    rolling: false
  };

  /* ---------------------------------------------------------------- 工具 */

  function toast(msg, isErr) {
    var el = $("toast");
    el.textContent = msg;
    el.className = "toast show" + (isErr ? " err" : "");
    clearTimeout(el._t);
    el._t = setTimeout(function () { el.className = "toast"; }, 2600);
  }

  function log(html) {
    var el = $("log");
    if (el.querySelector(".empty-marker")) { el.innerHTML = ""; }
    var time = new Date().toLocaleTimeString("zh-CN", { hour12: false });
    var line = document.createElement("div");
    line.innerHTML = '<span class="t">' + time + "</span>  " + html;
    el.appendChild(line);
    el.scrollTop = el.scrollHeight;
  }

  function pct(x) { return (x * 100).toFixed(2) + "%"; }

  function buildQuery(extra) {
    var q = new URLSearchParams();
    q.set("scenario", state.scenario);
    q.set("drawCount", String(state.drawCount));
    q.set("maxNumber", String(state.maxNumber));
    q.set("specialProb", String(state.prob / 100));
    if (extra) { Object.keys(extra).forEach(function (k) { q.set(k, extra[k]); }); }
    return q.toString();
  }

  /* -------------------------------------------------------------- 号码格 */

  var slots = [];
  function buildSlots(n) {
    var box = $("slots");
    box.innerHTML = "";
    slots = [];
    for (var i = 0; i < n; i++) {
      var d = document.createElement("div");
      d.className = "slot";
      var idx = document.createElement("span");
      idx.className = "idx";
      idx.textContent = i + 1;
      var val = document.createElement("span");
      val.textContent = "?";
      d.appendChild(idx);
      d.appendChild(val);
      box.appendChild(d);
      slots.push({ el: d, val: val });
    }
  }

  /* 滚动动画：每格独立随机跳号，间隔递增后依次 "锁定" 到真实结果。
     真实结果在动画开始前就已经从服务端拿到，动画只负责表现，不参与决定结果。 */
  function animate(items, special) {
    var REVEAL_STEP = 105;
    var TICK = 65;
    var maxDelay = 0;

    slots.forEach(function (s, i) {
      s.el.className = "slot rolling";
      s.val.textContent = String(items[i % items.length]);
    });

    slots.forEach(function (s, i) {
      var delay = i * REVEAL_STEP;
      if (delay > maxDelay) { maxDelay = delay; }

      var timer = setInterval(function () {
        s.val.textContent = String(1 + Math.floor(Math.random() * state.maxNumber));
      }, TICK);

      setTimeout(function () {
        clearInterval(timer);
        var target = items[i];
        s.val.textContent = String(target);
        s.el.className = "slot landed" + (target === special ? " special" : "");
      }, delay);
    });

    // 全部锁定后再做一次整理：重复出现的特殊号码统一样式。
    setTimeout(function () {
      var dupes = items.filter(function (x) { return x === special; }).length;
      if (dupes > 1) {
        slots.forEach(function (s, i) {
          if (items[i] === special) { s.el.classList.add("dupe"); }
        });
      }
    }, maxDelay + 250);

    return maxDelay + 420;
  }

  /* ------------------------------------------------------------ 票数统计 */

  var COLORS = {};
  function renderTally(stats, special, cfg) {
    var box = $("tally");
    box.className = "tally";
    box.innerHTML = "";
    var max = 1;
    Object.keys(stats.counts).forEach(function (k) {
      if (stats.counts[k] > max) { max = stats.counts[k]; }
    });
    if (stats.specialCount > max) { max = stats.specialCount; }

    for (var n = cfg.minNumber; n <= cfg.maxNumber; n++) {
      var c = stats.counts[String(n)] || 0;
      var row = document.createElement("div");
      row.className = "tally-row" + (n === special ? " is-special" : "");
      row.innerHTML =
        '<span class="num">' + n + "</span>" +
        '<span class="bar"><i style="width:' + Math.round((c / max) * 100) + '%"></i></span>' +
        '<span class="cnt">' + c + " 票" + (n === special ? " ★" : "") + "</span>";
      box.appendChild(row);
    }
  }

  function renderKV(pairs) {
    var dl = $("kv");
    dl.innerHTML = "";
    pairs.forEach(function (p) {
      var dt = document.createElement("dt");
      dt.textContent = p[0];
      var dd = document.createElement("dd");
      dd.innerHTML = p[1];
      dl.appendChild(dt);
      dl.appendChild(dd);
    });
  }

  function renderBadges(meta) {
    var b = $("badges");
    b.innerHTML =
      '<span class="badge live">● 服务在线 <b>' + meta.uptimeSeconds + "s</b></span>" +
      '<span class="badge">特殊号码 <b>' + meta.config.specialNumber + "</b></span>" +
      '<span class="badge">号码池 <b>' + meta.config.minNumber + "–" + meta.config.maxNumber + "</b></span>" +
      '<span class="badge">单轮命中率 <b>' + pct(meta.acceptProb) + "</b></span>" +
      '<span class="badge">期望重抽 <b>' + meta.expectedRounds.toFixed(2) + " 轮</b></span>";

    var notice = $("notice");
    if (meta.feasible === false) {
      notice.style.display = "";
      notice.textContent = "当前参数组合无解：特殊号码无法成为唯一最高票，抽奖请求会直接返回错误。" +
        "请提高特殊号码权重、扩大号码池，或减少一轮抽取次数。";
    } else if (meta.acceptProb < 0.02) {
      notice.style.display = "";
      notice.textContent = "当前参数下单轮自然命中率仅 " + pct(meta.acceptProb) +
        "，平均要重抽 " + meta.expectedRounds.toFixed(0) + " 轮。这属于压力测试区间，生产环境请放宽参数。";
    } else {
      notice.style.display = "none";
    }
  }

  function renderModes(modes) {
    var box = $("modes");
    box.innerHTML = "";
    modes.forEach(function (m) {
      var btn = document.createElement("button");
      btn.className = "pill" + (m.name === state.scenario ? " on" : "");
      btn.textContent = m.title;
      btn.onclick = function () {
        state.scenario = m.name;
        // 挑战模式回到 15%，其余模式使用各自内置权重。
        if (m.name === "challenging") { state.prob = 15; }
        if (m.name === "hot") { state.prob = 30; }
        if (m.name === "fair") { state.prob = Math.round(100 / (state.maxNumber - 1 + 1)); }
        syncControls();
        renderModes(modes);
        $("modeDesc").textContent = m.description;
        log('切换玩法 → <b>' + m.title + "</b>");
      };
      box.appendChild(btn);
    });
  }

  function syncControls() {
    $("probRange").value = String(state.prob);
    $("probVal").textContent = state.prob + "%";
    $("drawCountRange").value = String(state.drawCount);
    $("drawCountVal").textContent = String(state.drawCount);
    $("maxNumberRange").value = String(state.maxNumber);
    $("maxNumberVal").textContent = String(state.maxNumber);
  }

  /* ---------------------------------------------------------------- 抽奖 */

  function setBusy(on) {
    state.rolling = on;
    $("drawBtn").disabled = on;
    $("draw10Btn").disabled = on;
    $("simBtn").disabled = on;
  }

  function fetchJSON(url) {
    return fetch(url, { headers: { Accept: "application/json" } })
      .then(function (r) {
        return r.json().then(function (body) {
          if (!r.ok) { throw new Error(body && body.error ? body.error : "HTTP " + r.status); }
          return body;
        });
      });
  }

  function doDraw() {
    if (state.rolling) { return; }
    setBusy(true);
    $("statusBadge").textContent = "抽取中…";
    var t0 = performance.now();

    fetchJSON("/api/draw?" + buildQuery())
      .then(function (res) {
        var total = animate(res.items, res.config.specialNumber);
        renderTally(res.stats, res.config.specialNumber, res.config);
        $("attemptBadge").textContent = "内部重抽 " + res.attempts + " 轮";
        renderKV([
          ["抽奖 ID", "<code>" + res.id.slice(0, 12) + "…</code>"],
          ["玩法", res.scenario.title],
          ["特殊号码", "<b style='color:var(--gold)'>" + res.config.specialNumber + "</b>"],
          ["特殊号码票数", res.stats.specialCount + " 票"],
          ["最高票号码", res.stats.topNumber + "（" + res.stats.topCount + " 票）"],
          ["服务端耗时", res.elapsedMs.toFixed(2) + " ms"],
          ["往返耗时", (performance.now() - t0).toFixed(1) + " ms"]
        ]);
        log("抽奖成功 id=" + res.id.slice(0, 8) + " items=[" + res.items.join(",") +
            "] 重抽=" + res.attempts + " 轮" +
            (res.fallback ? ' <span class="err">（触发兜底构造）</span>' : ""));
        if (res.fallback) { toast("触发了兜底构造，请检查参数是否过紧", true); }
        setTimeout(function () {
          $("statusBadge").textContent = "已完成";
          setBusy(false);
        }, total);
      })
      .catch(function (e) {
        $("statusBadge").textContent = "失败";
        log('<span class="err">抽奖失败：' + e.message + "</span>");
        toast(e.message, true);
        setBusy(false);
      });
  }

  function doDraw10() {
    if (state.rolling) { return; }
    setBusy(true);
    $("statusBadge").textContent = "连抽中…";
    fetchJSON("/api/draw?" + buildQuery({ count: 10 }))
      .then(function (res) {
        var last = res.draws[res.draws.length - 1];
        var total = animate(last.items, res.config.specialNumber);
        renderTally(last.stats, res.config.specialNumber, res.config);
        $("attemptBadge").textContent = "10 轮 · 平均重抽 " + res.summary.averageAttempt.toFixed(2) + " 轮";
        renderKV([
          ["批量轮数", res.summary.rounds + " 轮"],
          ["平均内部重抽", res.summary.averageAttempt.toFixed(2) + " 轮"],
          ["兜底触发", res.summary.fallbackRounds + " 轮"],
          ["总耗时", res.elapsedMs.toFixed(2) + " ms"],
          ["每轮耗时", (res.elapsedMs / res.summary.rounds).toFixed(3) + " ms"]
        ]);
        res.draws.forEach(function (d, i) {
          log("第 " + (i + 1) + " 轮 items=[" + d.items.join(",") + "] 重抽=" + d.attempts + " 轮");
        });
        setTimeout(function () {
          $("statusBadge").textContent = "已完成";
          setBusy(false);
        }, total);
      })
      .catch(function (e) {
        $("statusBadge").textContent = "失败";
        log('<span class="err">连抽失败：' + e.message + "</span>");
        toast(e.message, true);
        setBusy(false);
      });
  }

  function doSimulate() {
    if (state.rolling) { return; }
    setBusy(true);
    $("statusBadge").textContent = "模拟中…";
    log("开始概率自检：2000 轮模拟…");

    fetchJSON("/api/simulate?" + buildQuery({ rounds: 2000 }))
      .then(function (res) {
        var rows = [];
        res.distribution.forEach(function (d) {
          rows.push([
            (d.isSpecial ? "★ " : "") + "号码 " + d.number,
            pct(d.share) + " <span style='color:var(--dim)'>(" + d.count + " 次)</span>"
          ]);
        });
        renderKV([
          ["模拟轮数", res.rounds + " 轮 × " + res.drawCount + " 次"],
          ["理论单轮命中率", pct(res.acceptProbTheory)],
          ["实测特殊号码最高票率", pct(res.specialTopRate)],
          ["实测特殊号码出现率", pct(res.specialSeenRate)],
          ["实测平均重抽", res.averageAttempt.toFixed(3) + " 轮"],
          ["模拟耗时", res.elapsedMs.toFixed(0) + " ms"]
        ].concat(rows));
        $("attemptBadge").textContent = "自检完成";
        log("自检完成：特殊号码最高票率 " + pct(res.specialTopRate) +
            "，理论单轮命中率 " + pct(res.acceptProbTheory));
        $("statusBadge").textContent = "自检完成";
        toast("自检完成，见右侧统计");
        setBusy(false);
      })
      .catch(function (e) {
        $("statusBadge").textContent = "失败";
        log('<span class="err">自检失败：' + e.message + "</span>");
        toast(e.message, true);
        setBusy(false);
      });
  }

  /* ---------------------------------------------------------------- 初始化 */

  function loadMeta() {
    return fetchJSON("/api/meta").then(function (meta) {
      state.meta = meta;
      state.drawCount = meta.config.drawCount;
      state.maxNumber = meta.config.maxNumber;
      state.prob = Math.round(meta.config.specialProb * 100);
      buildSlots(meta.config.drawCount);
      renderBadges(meta);
      renderModes(meta.modes);
      var def = meta.modes.filter(function (m) { return m.name === state.scenario; })[0];
      if (def) { $("modeDesc").textContent = def.description; }
      syncControls();
      log("已连接服务，配置：" + JSON.stringify(meta.config));
    });
  }

  $("drawBtn").onclick = doDraw;
  $("draw10Btn").onclick = doDraw10;
  $("simBtn").onclick = doSimulate;
  $("clearLogBtn").onclick = function () { $("log").innerHTML = ""; };

  $("probRange").oninput = function () {
    state.prob = Number(this.value);
    $("probVal").textContent = state.prob + "%";
  };
  $("drawCountRange").oninput = function () {
    state.drawCount = Number(this.value);
    $("drawCountVal").textContent = String(state.drawCount);
    buildSlots(state.drawCount);
  };
  $("maxNumberRange").oninput = function () {
    state.maxNumber = Number(this.value);
    $("maxNumberVal").textContent = String(state.maxNumber);
  };

  document.addEventListener("keydown", function (e) {
    if (e.code === "Space" && !/INPUT|BUTTON|SELECT/.test(document.activeElement.tagName)) {
      e.preventDefault();
      doDraw();
    }
  });

  loadMeta().catch(function (e) {
    log('<span class="err">无法连接服务：' + e.message + "</span>");
    toast("无法连接服务，请确认后端已启动", true);
  });
})();
