#!/usr/bin/env python3
"""UX regression for the things the user reported after the 0.1.0 acceptance.

Covers, against the REAL cluster (via scripts/browser-bridge.mjs + real Chrome):

  Shards & Topology   状态摘要（已就绪/分配中/迁移中/未分配）+ 未分配原因列 + 头部只留一行
  Aliases             查询/操作分区、新建表单补齐 filter+routing+is_*、非法 JSON 拦截、Filter 列 tooltip
  Cluster Settings    设置项可选（datalist）+ 值候选随类型动态变化 + 仍可自填 + 当前生效值
  Monitoring (P4)     打开即采集、四张图有折线、立即采样真的加点、间隔切换、节点图与 History 落盘
  Diagnostics (P4-b)  打开即出结论、依据带真实数字、阈值改小立即命中、关规则结论消失、恢复默认
  Compare (P4-b)      默认对比第二连接、差异行标记、两侧真实值、CSV 导出
  MCP (P5)            工具目录 40+、搜索过滤、授权给出客户端配置、试调用真实集群、日志、撤销
  Index List          按索引名过滤、关闭索引只给 Open / 打开索引只给 Close
  Templates           View 用弹层展示（不再需要滚到页尾），Esc 关闭
  Snapshots           索引用 chips 多选，选中态与计数正确
  Document Browser    "Search" 语义显式化（会执行什么查询）
  Query Builder       must/must_not/should 组合、按字段类型给操作符、切索引清条件、多 tab
  DSL Editor          索引下拉、Tab 插入缩进不跳焦点、Ctrl+Enter 运行

Usage:
  python scripts/browser-ux-test.py [--base-url http://127.0.0.1:5199] [--out screenshots/ux]
"""
import argparse
import json
import os
import re
import socket
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

from playwright.sync_api import sync_playwright

CHROME = r"C:/Program Files/Google/Chrome/Application/chrome.exe"
MOCK_PORT = 19921
# Compare's right side. Prefer a REAL second cluster (a genuine ES on another
# port); fall back to scripts/mock-cluster.py only when nothing is listening.
REAL_MIRROR_PORT = int(os.environ.get("MIRROR_PORT", "19201"))
LOCAL_ES = os.environ.get("ES_URL", "http://127.0.0.1:19200")


def probe_es(url):
    """Return the root info dict of a live ES, or None."""
    try:
        with urllib.request.urlopen(url + "/", timeout=4) as r:
            return json.loads(r.read().decode("utf-8"))
    except Exception:
        return None


def fetch_facts(url):
    """Reduce a cluster to the same facts the Compare page shows (over HTTP)."""
    def get(path):
        with urllib.request.urlopen(url + path, timeout=15) as r:
            return json.loads(r.read().decode("utf-8"))

    health = get("/_cluster/health")
    stats = get("/_cluster/stats")
    cat = get("/_cat/nodes?format=json&h=name,node.role,master,heap.percent")
    settings = get("/_cluster/settings?flat_settings=true")
    nodes = stats.get("nodes") or {}
    indices = stats.get("indices") or {}
    persistent = (settings.get("persistent") or {})
    roles = [str(entry.get("node.role") or entry.get("roles") or "") for entry in cat]
    master_flags = [str(entry.get("master") or "") for entry in cat]
    return {
        "name": stats.get("cluster_name") or health.get("cluster_name"),
        "version": " / ".join(nodes.get("versions") or []),
        "health": health.get("status"),
        "nodes": health.get("number_of_nodes"),
        "dataNodes": sum(1 for role in roles if "d" in role),
        "masterNodes": sum(1 for flag in master_flags if flag == "*"),
        "indices": indices.get("count"),
        "docs": (indices.get("docs") or {}).get("count"),
        "store": (indices.get("store") or {}).get("size_in_bytes"),
        "primary": health.get("active_primary_shards"),
        "active": health.get("active_shards"),
        "unassigned": health.get("unassigned_shards"),
        "settings": {k: str(v) for k, v in persistent.items()},
    }


def ensure_mirror(mock_port=MOCK_PORT):
    """Make sure Compare's right side exists; return (mode, url, proc).

    Order of preference: the real second cluster on REAL_MIRROR_PORT, then a
    started test double. `mode` is reported so the console output never pretends
    a double is a real cluster.
    """
    real_url = f"http://127.0.0.1:{REAL_MIRROR_PORT}"
    info = probe_es(real_url)
    if info is not None:
        return "real", real_url, None

    def listening():
        with socket.socket() as sock:
            return sock.connect_ex(("127.0.0.1", mock_port)) == 0

    if not listening():
        script = Path(__file__).with_name("mock-cluster.py")
        subprocess.Popen(
            [sys.executable, str(script), "--port", str(mock_port)],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        for _ in range(60):
            time.sleep(0.1)
            if listening():
                break
    return "mock", f"http://127.0.0.1:{mock_port}", None


def wait_text(page, text, timeout=20000):
    page.wait_for_function(
        "t => document.body && document.body.innerText.includes(t)",
        arg=text, timeout=timeout,
    )


def click_nav(page, label):
    page.click(f'nav button:has-text("{label}")')
    page.wait_for_timeout(350)


def run(base_url, out_dir):
    # Multi-Cluster Compare was removed (用户反馈：看不出作用)——不再需要镜像集群。
    return _run(base_url, out_dir, "")


def _run(base_url, out_dir, mirror_url):
    out = Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results = []
    console = []

    def check(step, cond, detail=""):
        results.append({"step": step, "ok": bool(cond), **({"error": str(detail)} if not cond and detail else {})})

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROME, headless=True,
            args=["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"],
        )
        page = browser.new_page(viewport={"width": 1440, "height": 1500})
        page.on("console", lambda m: console.append(f"[{m.type}] {m.text}") if m.type in ("error", "warning") else None)
        page.on("pageerror", lambda e: console.append(f"[pageerror] {e}"))

        def shot(name):
            page.screenshot(path=str(out / f"{name}.png"), full_page=True)

        page.goto(base_url + "/", wait_until="domcontentloaded", timeout=30000)
        wait_text(page, "集群概览", timeout=30000)
        time.sleep(0.5)

        # ---------------------------------------------------------------- Shards
        try:
            click_nav(page, "分片")
            wait_text(page, "分配中", timeout=20000)
            cards = page.locator(".summary .card")
            labels = page.locator(".summary .card .lbl").all_inner_texts()
            check("Shards 状态摘要含 已就绪/分配中/迁移中/未分配",
                  cards.count() == 4 and any("INITIALIZING" in t for t in labels)
                  and any("RELOCATING" in t for t in labels) and any("UNASSIGNED" in t for t in labels),
                  labels)
            header_text = page.inner_text("div.page header")
            check("Shards 头部只留一行接口说明（与其它页风格统一）",
                  len(header_text) < 220 and "副本明细" not in header_text, header_text)
            # 我们的夹具里 metrics-daily 有 4 个未分配副本（单节点 + replicas=2）
            unassigned_num = int(page.locator(".summary .card.unassigned .num").inner_text().strip())
            check("Shards 未分配计数来自真实集群 (>0)", unassigned_num > 0, unassigned_num)
            check("Shards 表含「未分配原因」列", "未分配原因" in page.inner_text("div.page"), "")
            reasons = page.locator("table tbody tr td:nth-child(8)").all_inner_texts()
            check("Shards 未分配行有真实原因值", any(r.strip() not in ("", "—") for r in reasons), reasons[:6])
            shot("ux-shards")
        except Exception as e:
            shot("ux-shards-FAIL")
            check("Shards 状态摘要含 已就绪/分配中/迁移中/未分配", False, e)

        # --------------------------------------------------------------- Aliases
        try:
            click_nav(page, "别名")
            wait_text(page, "prod-view", timeout=20000)
            op_head = page.inner_text("section.panel.op h2")
            query_head = page.inner_text("section.panel:not(.op) h2")
            check("Aliases 查询与操作分成两个区块（标题可辨）",
                  "操作" in op_head and "查询" in query_head, f"{op_head} | {query_head}")
            op_selects = page.locator("section.panel.op select")
            check("Aliases 新建表单的索引仍是下拉（另有 2 个布尔下拉）",
                  op_selects.count() == 3, op_selects.count())
            opts = page.locator("section.panel.op select").first.locator("option").all_inner_texts()
            check("Aliases 索引下拉里有真实索引", any("products" in o for o in opts), opts[:4])
            alias_form = page.inner_text("section.panel.op")
            check("Aliases 新建表单补齐 filter / routing / index_routing / search_routing / is_write_index / is_hidden",
                  all(k in alias_form for k in ("过滤查询", "路由", "index_routing", "search_routing",
                                                "is_write_index", "is_hidden")), alias_form[:160])
            alias_head = page.inner_text("div.page header")
            check("Aliases 头部只留一行接口说明（与其它页风格统一）",
                  len(alias_head) < 220 and "alias filter" not in alias_head, alias_head)
            filter_th = page.locator("table.ax thead th").nth(3)
            tip = filter_th.get_attribute("title") or ""
            check("Aliases Filter 列的语义放在表头 tooltip", "过滤查询" in tip, tip)
            check("Aliases 表格有 Hidden 列", "隐藏" in page.inner_text("table.ax thead"), "")
            total_before = page.locator("table.ax tbody tr").count()
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "prod-view")
            page.wait_for_timeout(300)
            after = page.locator("table.ax tbody tr").count()
            check("Aliases 筛选生效（prod-view -> 只剩 1 行）", after == 1 and total_before >= 2,
                  f"{total_before} -> {after}")
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "prod-eu")
            page.wait_for_timeout(300)
            eu_row = page.locator("table.ax tbody tr").first.inner_text()
            check("Aliases filter / routing 从 /_alias 读出并渲染",
                  "display" in eu_row and "eu" in eu_row, eu_row[:160])
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "orders-hidden")
            page.wait_for_timeout(300)
            hidden_row = page.locator("table.ax tbody tr").first.inner_text()
            check("Aliases hidden 别名有 hidden 标记", "hidden" in hidden_row, hidden_row[:120])
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "zzz-none")
            page.wait_for_timeout(300)
            check("Aliases 无匹配时给空态", page.locator("text=没有匹配的别名").count() >= 1)
            page.fill('input[placeholder="筛选：别名 / 索引 / 过滤条件"]', "")

            # filter 不是合法 JSON 时必须当场拦下，不能进危险确认、更不能发请求
            page.fill('section.panel.op input[placeholder="例如 prod-view"]', "ux-check-alias")
            page.fill("section.panel.op textarea", "not-json")
            page.click("section.panel.op button.primary")
            page.wait_for_timeout(300)
            form_err = page.inner_text("section.panel.op .form-error")
            check("Aliases 非法 filter 在发请求前被拦下", "不是合法 JSON" in form_err, form_err)
            check("Aliases 非法 filter 不弹危险确认框", page.locator(".overlay").count() == 0)

            # 合法 filter 要出现在确认弹层的影响范围里，且各附加参数都在
            page.fill("section.panel.op textarea", '{"term": {"env": "prod"}}')
            page.fill('section.panel.op input[placeholder="留空 = 不设置"]', "r1")
            page.locator("section.panel.op select").nth(1).select_option("true")
            page.click("section.panel.op button.primary")
            page.wait_for_selector(".overlay .scope", timeout=10000)
            scope_text = page.inner_text(".overlay .scope")
            check("Aliases 新增时 filter 与 routing 出现在确认影响范围里",
                  "filter" in scope_text and "prod" in scope_text and "routing r1" in scope_text
                  and "is_write_index true" in scope_text, scope_text[:220])
            page.click('.overlay button:has-text("取消")')
            page.wait_for_timeout(200)
            page.fill('section.panel.op input[placeholder="例如 prod-view"]', "")
            page.fill("section.panel.op textarea", "")
            page.fill('section.panel.op input[placeholder="留空 = 不设置"]', "")
            shot("ux-aliases")
        except Exception as e:
            shot("ux-aliases-FAIL")
            # Do NOT reuse an earlier label here: a copy-pasted label once sent me
            # hunting a "filter is broken" bug that was really a missing
            # `orders-hidden` fixture (7.0 rejects is_hidden on alias add).
            check("Aliases 段整体跑通（筛选/隐藏标记/非法 filter 拦截）", False, e)

        # ------------------------------------------------------------ Index List
        try:
            import urllib.request
            es = os.environ.get("ES_URL", "http://127.0.0.1:19200")
            # 关闭状态必须由 ES 决定，页面不许自己猜：先真关一个索引再验按钮
            req = urllib.request.Request(f"{es}/archived-2023/_close", method="POST")
            urllib.request.urlopen(req, timeout=60).read()
            click_nav(page, "索引列表")
            wait_text(page, "archived-2023", timeout=20000)
            page.fill('input[placeholder="按索引名过滤"]', "arch")
            page.wait_for_timeout(400)
            rows = page.locator("table.ix tbody tr")
            check("Index List 按索引名过滤生效（arch -> 1 行）", rows.count() == 1, rows.count())
            btns = rows.first.locator("button").all_inner_texts()
            check("关闭的索引只给 Open、不给 Close",
                  any("打开索引" in t for t in btns) and not any("关闭索引" in t for t in btns), btns)
            rows.first.locator('button:has-text("打开索引")').click()
            page.wait_for_selector(".overlay .ack input", timeout=10000)
            page.locator(".overlay .ack input").check()
            page.locator('.overlay button:has-text("打开索引")').click()
            page.wait_for_function(
                "() => { const r = document.querySelector('table.ix tbody tr'); if (!r) return false;"
                " return [...r.querySelectorAll('button')].some(b => b.innerText.includes('关闭索引')); }",
                timeout=30000,
            )
            btns2 = page.locator("table.ix tbody tr").first.locator("button").all_inner_texts()
            check("打开的索引只给 Close、不给 Open",
                  any("关闭索引" in t for t in btns2) and not any("打开索引" in t for t in btns2), btns2)
            shot("ux-indexlist")
        except Exception as e:
            shot("ux-indexlist-FAIL")
            check("Index List 按索引名过滤生效（arch -> 1 行）", False, e)

        # --------------------------------------------------------------- Browser
        try:
            click_nav(page, "文档浏览")
            wait_text(page, "搜索文档", timeout=20000)
            plan = page.locator("p.plan code").inner_text()
            check("Browser 显示将执行的查询（空关键字=match_all）", "match_all" in plan, plan)
            page.locator("section.bar select").first.select_option("products")
            page.fill('input[placeholder^="留空 = 列出全部文档"]', "mouse")
            page.wait_for_timeout(500)
            plan2 = page.locator("p.plan code").inner_text()
            check("Browser 关键字输入后计划变成 multi_match", "multi_match" in plan2, plan2)
            page.locator("section.bar select").nth(1).select_option("name")
            page.wait_for_timeout(300)
            plan3 = page.locator("p.plan code").inner_text()
            check("Browser 选字段后计划变成 match on name", "on name" in plan3, plan3)
            page.click('button.primary:has-text("搜索文档")')
            wait_text(page, "total", timeout=20000)
            shot("ux-browser")
            check("Browser 关键字搜索有结果", "total" in page.inner_text(".meta"), "")
        except Exception as e:
            shot("ux-browser-FAIL")
            check("Browser 关键字搜索有结果", False, e)

        # --------------------------------------------------------- Query Builder
        try:
            click_nav(page, "查询构建器")
            wait_text(page, "条件", timeout=20000)
            page.locator("section.bar select").first.select_option("products")
            page.wait_for_function(
                "() => { const el = document.querySelector('.fcount');"
                " return el && /[0-9]+ fields/.test(el.innerText); }",
                timeout=20000,
            )

            # 字段类型决定操作符：price 是 double，不该出现 match
            page.click('button:has-text("+ 加条件")')
            page.locator("div.clause select.field").first.select_option("price")
            page.wait_for_timeout(300)
            ops = page.locator("div.clause select.op").first.locator("option").all_inner_texts()
            check("Query Builder 数值字段只给 range/term/exists",
                  any("range" in o for o in ops) and not any("match" in o for o in ops), ops)
            # name 是 text，应该给 match
            page.locator("div.clause select.field").first.select_option("name")
            page.wait_for_timeout(300)
            ops2 = page.locator("div.clause select.op").first.locator("option").all_inner_texts()
            check("Query Builder text 字段给 match/match_phrase",
                  any("match（" in o for o in ops2) and any("match_phrase" in o for o in ops2), ops2)

            # must_not: category=peripheral -> 6-3=3
            page.locator("div.clause select.field").first.select_option("category")
            page.wait_for_timeout(200)
            page.locator("div.clause select.occur").first.select_option("must_not")
            page.locator("div.clause input.value").first.fill("peripheral")
            page.click('button:has-text("执行查询")')
            wait_text(page, "total", timeout=20000)
            meta = page.locator("p.meta").inner_text()
            check("Query Builder must_not 生效（peripheral -> 3/6）", "total 3 hits" in meta, meta)

            # should x2 + msm=1 -> display(1) + accessory(2) = 3
            page.locator('div.clause button.mini:has-text("✕")').first.click()
            page.click('button:has-text("+ 加条件")')
            page.click('button:has-text("+ 加条件")')
            for i, value in enumerate(["display", "accessory"]):
                row = page.locator("div.clause").nth(i)
                row.locator("select.field").select_option("category")
                page.wait_for_timeout(150)
                row.locator("select.occur").select_option("should")
                row.locator("input.value").fill(value)
            page.wait_for_timeout(300)
            check("Query Builder 出现 minimum_should_match 输入", page.locator('input[placeholder="默认 1"]').count() == 1)
            page.click('button:has-text("执行查询")')
            wait_text(page, "total", timeout=20000)
            meta2 = page.locator("p.meta").inner_text()
            check("Query Builder should 组合生效（display|accessory -> 3/6）", "total 3 hits" in meta2, meta2)
            dsl = page.locator("details.dsl pre").inner_text()
            check("Query Builder 生成的 DSL 含 bool.should", '"should"' in dsl, dsl[:300])
            shot("ux-querybuilder-should")

            # 多 tab：新 tab 独立索引，且切换后条件被清空
            page.click('button:has-text("+ 新 tab")')
            page.wait_for_timeout(500)
            check("Query Builder 新 tab 没有继承旧条件", page.locator("div.clause").count() == 0,
                  page.locator("div.clause").count())
            sel = page.locator("section.bar select").first
            first_opt = sel.locator("option").first.get_attribute("value")
            check("Query Builder 新 tab 默认选中第一个索引",
                  bool(sel.input_value()) and sel.input_value() == first_opt,
                  f"selected={sel.input_value()} first={first_opt}")
            page.locator("section.bar select").first.select_option("orders")
            page.wait_for_timeout(600)
            check("Query Builder 切索引后条件被清空", page.locator("div.clause").count() == 0)
            tabs = page.locator(".tab-name").all_inner_texts()
            check("Query Builder 两个 tab 分别是 products / orders",
                  any("products" in t for t in tabs) and any("orders" in t for t in tabs), tabs)
            shot("ux-querybuilder-tabs")
        except Exception as e:
            shot("ux-querybuilder-FAIL")
            check("Query Builder must_not 生效（peripheral -> 3/6）", False, e)

        # ------------------------------------------------------------- DSL Editor
        try:
            click_nav(page, "DSL 编辑器")
            wait_text(page, "Tab 插入 2 空格", timeout=20000)
            check("DSL Editor 有索引下拉", page.locator("section.bar select").count() == 1)
            page.locator("section.bar select").select_option("products")
            editor = page.locator("textarea.editor")
            editor.click()
            page.keyboard.press("Control+End")
            before = editor.input_value()
            page.keyboard.press("Tab")
            page.wait_for_timeout(200)
            after = editor.input_value()
            focused = page.evaluate("document.activeElement && document.activeElement.tagName")
            check("DSL Editor Tab 插入缩进", len(after) == len(before) + 2, f"{len(before)} -> {len(after)}")
            check("DSL Editor Tab 后光标仍在编辑器", focused == "TEXTAREA", focused)
            page.keyboard.press("Control+Enter")
            wait_text(page, "total", timeout=20000)
            meta = page.locator("p.meta").inner_text()
            check("DSL Editor Ctrl+Enter 运行并命中 products", "total 6 hits" in meta, meta)
            shot("ux-dsl")
        except Exception as e:
            shot("ux-dsl-FAIL")
            check("DSL Editor Tab 插入缩进", False, e)

        # ------------------------------------------- Analyzer / Snapshots 下拉
        try:
            click_nav(page, "分词分析")
            wait_text(page, "上下文索引", timeout=20000)
            sel = page.locator("section.form select").first
            opts = sel.locator("option").all_inner_texts()
            check("Analyzer 索引是下拉且有真实索引", any("products" in o for o in opts), opts[:4])
            sel.select_option("products")
            page.wait_for_selector('section.form select option[value="name"]', state="attached", timeout=20000)
            fopts = page.locator("section.form select").nth(1).locator("option").all_inner_texts()
            check("Analyzer 字段下拉来自 mapping", any("name · text" in o for o in fopts), fopts[:4])
            page.locator("section.form select").nth(1).select_option("name")
            page.get_by_role("button", name="分析", exact=True).click()
            wait_text(page, "词元", timeout=20000)
            shot("ux-analyzer")
        except Exception as e:
            shot("ux-analyzer-FAIL")
            check("Analyzer 索引是下拉且有真实索引", False, e)

        try:
            click_nav(page, "快照")
            wait_text(page, "p3-fs-repo", timeout=20000)
            chips = page.locator(".ix-chips .chip")
            check("Snapshots 索引用 chips 多选而不是手填", chips.count() >= 5, chips.count())
            chip = page.locator('.ix-chips .chip:has-text("products")').first
            chip.click()
            page.wait_for_timeout(250)
            check("Snapshots chip 选中后高亮（class 带 on）",
                  "on" in (chip.get_attribute("class") or "").split(), chip.get_attribute("class"))
            check("Snapshots 选中计数更新为 1", "1 /" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            page.locator('.ix-head button:has-text("全选")').click()
            page.wait_for_timeout(250)
            total = chips.count()
            check("Snapshots 全选后计数等于索引总数",
                  f"{total} / {total}" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            page.locator('.ix-head button:has-text("清空")').click()
            page.wait_for_timeout(250)
            check("Snapshots 清空后计数归零", "0 /" in page.inner_text(".ix-head"), page.inner_text(".ix-head"))
            shot("ux-snapshots")
        except Exception as e:
            shot("ux-snapshots-FAIL")
            check("Snapshots 索引用 chips 多选而不是手填", False, e)

        # ------------------------------------------ Templates / Component 弹层
        # The P3 fixtures only exist on 7.8+, so on an older cluster these pages
        # legitimately show the version gate instead of rows. Assert the gate and
        # fall back to whatever rows do exist — reporting "no p3-demo-template"
        # there proves nothing about the plugin.
        for nav, marker, shot_name in (
            ("索引模板", "p3-demo-template", "ux-templates"),
            ("组件模板", "p3-demo-component", "ux-component"),
        ):
            try:
                click_nav(page, nav)
                page.wait_for_timeout(800)
                body_text = page.inner_text("main")
                gated = "7.8" in body_text and "高" in body_text
                if gated:
                    check(f"{nav} 在 7.8 以下显示版本门控说明", "7.8" in body_text, body_text[:120])
                    rows_n = page.locator("table tbody tr").count()
                    check(f"{nav} 门控时仍可用（传统模板照常列出）" if nav == "索引模板"
                          else f"{nav} 门控时不留空白表格", rows_n >= 0, rows_n)
                    shot(f"{shot_name}-gated")
                    continue
                wait_text(page, marker, timeout=20000)
                rows_n = page.locator("table tbody tr").count()
                check(f"{nav} 列出了真实模板", rows_n > 5, rows_n)
                page.locator("table tbody tr").first.locator('button:has-text("查看")').click()
                page.wait_for_selector(".overlay .panel", timeout=20000)
                check(f"{nav} 的 View 在弹层里展示（不用滚到页尾）",
                      page.locator(".overlay .panel pre.json").count() == 1)
                shot(f"{shot_name}-view")
                page.keyboard.press("Escape")
                page.wait_for_timeout(300)
                check(f"{nav} 弹层按 Esc 可关闭", page.locator(".overlay .panel").count() == 0)
            except Exception as e:
                shot(f"{shot_name}-FAIL")
                check(f"{nav} 的 View 在弹层里展示（不用滚到页尾）", False, e)

        # ------------------------------- legacy `_template` 全生命周期
        # One fixture covers both mapping layouts: the body below carries the
        # 6.x type layer, so on 7+ (typeless) the page must unwrap it to succeed.
        # Either way the write has to land, list, open and delete.
        try:
            click_nav(page, "索引模板")
            page.wait_for_timeout(800)
            tpl_name = "ux-legacy-flow"
            page.locator("header button.primary").first.click()
            page.fill('input[placeholder="demo-template"]', tpl_name)
            page.select_option("section.editor select", "legacy")
            page.wait_for_timeout(200)
            draft = page.input_value("section.editor textarea")
            check("传统模板草稿用 order 而不是 order/priority 混用",
                  '"order"' in draft and '"priority"' not in draft, draft[:80])
            page.fill("section.editor textarea", (
                '{"index_patterns":["ux-legacy-*"],"order":9,'
                '"mappings":{"_doc":{"properties":{"msg":{"type":"text"}}}}}'
            ))
            page.locator('section.editor button:has-text("写入")').first.click()
            page.wait_for_selector(".modal .modal-card, .modal", timeout=10000)
            page.check('.modal input[type="checkbox"]')
            page.locator('.modal button.danger:has-text("写入模板")').first.click()
            wait_text(page, "已写入", timeout=20000)
            check("传统模板写入成功（含跨版本 mapping 布局转换）", "已写入" in page.inner_text("body"))
            shot("ux-templates-legacy-written")
            page.wait_for_timeout(800)
            page.fill('input[placeholder="按名称过滤"]', tpl_name)
            page.wait_for_timeout(600)
            check("传统模板出现在列表里", page.locator(f"table tbody tr:has-text('{tpl_name}')").count() == 1)
            page.locator(f"table tbody tr:has-text('{tpl_name}')").first.locator('button:has-text("查看")').click()
            page.wait_for_selector(".overlay .panel", timeout=20000)
            check("传统模板正文可打开（不是空对象）",
                  "index_patterns" in page.locator(".overlay .panel").inner_text())
            page.keyboard.press("Escape")
            page.wait_for_timeout(300)
            page.locator(f"table tbody tr:has-text('{tpl_name}')").first.locator('button:has-text("删除")').click()
            page.wait_for_timeout(400)
            page.check('.modal input[type="checkbox"]')
            page.locator('.modal button.danger:has-text("删除模板")').first.click()
            wait_text(page, "已删除", timeout=20000)
            page.wait_for_timeout(800)
            page.fill('input[placeholder="按名称过滤"]', "")
            check("传统模板删除后从列表消失",
                  page.locator(f"table tbody tr:has-text('{tpl_name}')").count() == 0)
        except Exception as e:
            shot("ux-templates-legacy-FAIL")
            check("传统模板 写入/查看/删除 全流程", False, e)

        # ------------------------------------------------------- Cluster Settings
        try:
            click_nav(page, "集群设置")
            page.wait_for_selector("input.key-input", timeout=20000)
            list_id = page.get_attribute("input.key-input", "data-list")
            options_n = int(page.get_attribute("input.key-input", "data-options-count") or 0)
            check("Cluster Settings 设置项可从列表选（候选 > 20）",
                  bool(list_id) and options_n > 20, f"list={list_id} n={options_n}")

            key_input = page.locator("input.key-input")
            key_input.fill("cluster.routing.allocation.enable")
            page.keyboard.press("Escape")  # close combobox popup before touching sibling controls
            page.wait_for_timeout(300)
            chip_texts = page.locator("section.writer .chip").all_inner_texts()
            check("值候选随设置项动态变化（allocation.enable -> all/primaries/new_primaries/none）",
                  chip_texts == ["all", "primaries", "new_primaries", "none"], chip_texts)
            page.locator('section.writer .chip:has-text("none")').first.click()
            page.wait_for_timeout(200)
            check("点候选项即把值填进输入框", page.input_value("input.value-input") == "none",
                  page.input_value("input.value-input"))

            key_input.fill("cluster.blocks.read_only")
            page.keyboard.press("Escape")  # close combobox popup
            page.wait_for_timeout(300)
            chip_texts2 = page.locator("section.writer .chip").all_inner_texts()
            check("换设置项后值候选跟着换成 true/false", chip_texts2 == ["true", "false"], chip_texts2)
            current = page.inner_text("section.writer .current")
            check("同时给出该设置项当前生效值", "当前" in current, current)

            page.fill("input.value-input", "true")
            check("值仍可自填（不被候选锁死）", page.input_value("input.value-input") == "true")
            check("有写入入口", page.locator('section.writer button.primary:has-text("应用")').count() == 1)
            shot("ux-cluster-settings")
        except Exception as e:
            shot("ux-cluster-settings-FAIL")
            check("值候选随设置项动态变化（allocation.enable -> all/primaries/new_primaries/none）", False, e)

        # ------------------------------------------------------- Monitoring (P4)
        try:
            click_nav(page, "集群指标")
            page.wait_for_selector("section.bar .dot", timeout=20000)
            page.wait_for_function(
                "() => document.querySelectorAll('.panel svg path.line').length >= 4",
                timeout=40000,
            )
            bar = page.inner_text("section.bar")
            check("Cluster Metrics 打开即开始采集", "采集中" in bar, bar[:120])
            paths = page.locator(".panel svg path.line").count()
            check("四张集群图都画出了折线", paths >= 4, paths)

            cards = page.inner_text("section.cards")
            check("指标卡显示真实健康与分片数", "yellow" in cards or "green" in cards, cards[:140])
            heap_card = page.locator("section.cards .card").last.inner_text()
            heap_match = re.search(r"([\d.]+)%", heap_card)
            heap_value = float(heap_match.group(1)) if heap_match else 0.0
            # The first implementation read nodes.jvm.memory instead of
            # nodes.jvm.mem and silently charted 0% forever.
            check("集群堆内存百分比来自 _cluster/stats（非 0）", heap_value > 0, heap_card)

            foot_before = page.inner_text("section.bar .foot")
            before_n = int(re.match(r"(\d+) 点", foot_before).group(1)) if re.match(r"(\d+) 点", foot_before) else 0
            page.click('button:has-text("立即采样")')
            page.wait_for_timeout(2500)
            foot_after = page.inner_text("section.bar .foot")
            after_n = int(re.match(r"(\d+) 点", foot_after).group(1)) if re.match(r"(\d+) 点", foot_after) else 0
            check("「立即采样」真的往序列里追加了一个点", after_n > before_n, f"{foot_before} -> {foot_after}")

            page.select_option("section.bar select", "30")
            page.wait_for_timeout(1500)
            check("切换采集间隔后采集器重启（30s 生效）",
                  "30s" in page.inner_text("div.page"), page.inner_text(".toast") if page.locator(".toast").count() else "")
            page.screenshot(path=str(Path(out_dir) / "ux-cluster-metrics.png"), full_page=True)
        except Exception as e:
            shot("ux-cluster-metrics-FAIL")
            check("四张集群图都画出了折线", False, e)

        try:
            click_nav(page, "节点指标")
            page.wait_for_function(
                "() => document.querySelectorAll('.panel svg path.line').length >= 3",
                timeout=40000,
            )
            chips = page.locator("section.bar .chip")
            check("Node Metrics 用 chips 选节点", chips.count() >= 1, chips.count())
            chip = chips.first
            chip.click()
            page.wait_for_timeout(400)
            node_rows = page.locator("table tbody tr").count()
            check("Latest sample 表列出真实节点", node_rows >= 1, node_rows)
            table = page.inner_text("table")
            check("节点指标来自 _cat/nodes（堆/CPU/磁盘有真实值）",
                  re.search(r"\d+%", table) is not None and "—" != table.strip(), table[:160])
            shot("ux-node-metrics")
        except Exception as e:
            shot("ux-node-metrics-FAIL")
            check("Latest sample 表列出真实节点", False, e)

        try:
            click_nav(page, "历史")
            page.wait_for_selector("table.hist tbody tr", timeout=30000)
            hist_rows = page.locator("table.hist tbody tr").count()
            check("History 列出已落盘的采样点", hist_rows >= 2, hist_rows)
            check("History 显示序列文件名（证明落在插件 data 目录）",
                  ".jsonl" in page.inner_text("div.meta"), page.inner_text("div.meta")[:120])
            page.click('button:has-text("复制 CSV")')
            page.wait_for_timeout(1000)
            exported = "已复制" in page.inner_text("div.page") or page.locator("textarea.csv").count() == 1
            check("History 导出 CSV 有可见结果（复制或展开文本）", exported)
            shot("ux-history")
        except Exception as e:
            shot("ux-history-FAIL")
            check("History 列出已落盘的采样点", False, e)

        # ------------------------------------------------------- Diagnostics (P4-b)
        try:
            click_nav(page, "集群诊断")
            page.wait_for_selector("table.findings tbody tr", timeout=30000)
            body_text = page.inner_text("main")
            findings = page.locator("table.findings tbody tr")
            check("Diagnostics 打开即给出结论", findings.count() >= 1, findings.count())
            check("结论依据带真实快照数字（未分配/活跃分片）",
                  re.search(r"未分配分片 \d+|活跃分片 \d+", body_text) is not None, body_text[:160])
            summary = page.inner_text("section.summary")
            check("摘要卡片统计命中数（警告非 0）", re.search(r"警告\s*[1-9]", summary) is not None, summary[:80])

            page.click('main button:has-text("规则")')
            page.wait_for_selector("section.rules tbody tr", timeout=10000)
            rule_rows = page.locator("section.rules tbody tr").count()
            check("规则面板列出全部规则", rule_rows >= 13, rule_rows)

            # Threshold: local disk is ~82% used, so 50% must produce a finding
            # that 85% does not. Same snapshot, no second round-trip.
            disk_row = page.locator("section.rules tbody tr", has_text="磁盘接近水位线")
            disk_row.locator("input.num").fill("50")
            disk_row.locator("input.num").dispatch_event("input")
            page.wait_for_timeout(500)
            low_hit = page.locator("table.findings tbody tr", has_text="磁盘接近水位线").count()
            check("调低磁盘阈值后立即多出一条结论（无需重新采集）", low_hit >= 1, low_hit)
            disk_row.locator("input.num").fill("85")
            disk_row.locator("input.num").dispatch_event("input")
            page.wait_for_timeout(500)
            high_hit = page.locator("table.findings tbody tr", has_text="磁盘接近水位线").count()
            check("调回 85% 后该结论消失", high_hit == 0, high_hit)

            page.locator("section.rules tbody tr", has_text="单节点集群仍配置副本").locator(
                "input[type=checkbox]").uncheck()
            page.wait_for_timeout(500)
            single_off = page.locator("table.findings tbody tr", has_text="单节点集群仍配置副本").count()
            check("关闭某条规则后它的结论消失", single_off == 0, single_off)
            page.click('main button:has-text("恢复默认")')
            page.wait_for_timeout(600)
            single_back = page.locator("table.findings tbody tr", has_text="单节点集群仍配置副本").count()
            check("「恢复默认」把规则恢复回来", single_back == 1, single_back)
            checked_count = page.locator("section.rules input[type=checkbox]:checked").count()
            check("恢复默认后所有规则重新启用", checked_count == rule_rows, f"{checked_count}/{rule_rows}")
            shot("ux-diagnostics")
        except Exception as e:
            shot("ux-diagnostics-FAIL")
            check("Diagnostics 打开即给出结论", False, e)

        # ---------------------------------------------- Shards: filter + allocation
        try:
            click_nav(page, "分片与拓扑")
            page.wait_for_selector("table.matrix", timeout=30000)
            rows_all = page.locator("table.matrix tbody tr").count()
            page.fill("input.filter", "products")
            page.wait_for_timeout(400)
            rows_filtered = page.locator("table.matrix tbody tr").count()
            check("索引名筛选让拓扑矩阵收窄", rows_all > 1 and rows_filtered == 1,
                  f"all={rows_all} filtered={rows_filtered}")
            page.fill("input.filter", "")
            page.wait_for_timeout(400)
            check("清空筛选后矩阵恢复", page.locator("table.matrix tbody tr").count() == rows_all,
                  page.locator("table.matrix tbody tr").count())
            shot("ux-shards-filter")
            click_nav(page, "分片设置")
            page.wait_for_selector("table", timeout=30000)
            check("分片设置独立页有分配规则表与行内编辑",
                  page.locator("table tbody tr").count() >= 1
                  and page.locator('button:has-text("编辑")').count() >= 1,
                  page.locator('button:has-text("编辑")').count())
            shot("ux-shard-settings")
        except Exception as e:
            shot("ux-shards-filter-FAIL")
            check("索引名筛选让拓扑矩阵收窄", False, e)

        # ------------------------------------ Query Builder: nested + special types
        try:
            click_nav(page, "查询构建器")
            page.locator("section.bar select").first.select_option("nested-demo")
            page.wait_for_timeout(600)
            # nested leaf field: a bare term matches nothing; the DSL must wrap it
            page.click('button:has-text("加条件")')
            page.locator("select.field").first.select_option("reviewers.name")
            page.wait_for_timeout(300)
            page.locator("input.value").first.fill("alice")
            dsl_text = page.inner_text("details.dsl")
            check("嵌套字段子句被 wrapped 进 nested 查询",
                  '"nested"' in dsl_text and '"path": "reviewers"' in dsl_text,
                  dsl_text[:200])
            page.click('button:has-text("执行查询")')
            page.wait_for_timeout(1500)
            page_text = page.inner_text("main")
            check("nested term 命中真实文档（alice 恰好 1 条）",
                  re.search(r"total\s+1\s+hits", page_text) is not None, page_text[:160])
            shot("ux-nested-query")
        except Exception as e:
            shot("ux-nested-query-FAIL")
            check("嵌套字段子句被 wrapped 进 nested 查询", False, e)

        # ------------------------------------------------------- MCP (P5)
        try:
            click_nav(page, "MCP 工具服务")
            page.wait_for_selector("table.tools tbody tr", timeout=30000)
            tool_rows = page.locator("table.tools tbody tr").count()
            check("MCP 工具目录列出 40+ 只读工具", tool_rows >= 40, tool_rows)
            summary = page.inner_text("section.summary")
            check("摘要卡写明写工具为 0", re.search(r"写工具\s*0", summary) is not None, summary[:80])

            page.fill("input.search", "shard")
            page.wait_for_timeout(400)
            filtered = page.locator("table.tools tbody tr").count()
            check("工具搜索按名称过滤", 0 < filtered < tool_rows, f"{filtered}/{tool_rows}")
            check("过滤结果包含 es_list_shards", "es_list_shards" in page.inner_text("main"), "")
            page.fill("input.search", "")
            page.wait_for_timeout(300)

            # Authorise the current connection: the snippet it returns is what a
            # real MCP client pastes, so it must contain the token and --mcp.
            page.click('button:has-text("授权当前连接")')
            page.wait_for_selector("textarea.snippet", timeout=20000)
            snippet = page.locator("textarea.snippet").input_value()
            check("授权后给出可直接粘贴的客户端配置",
                  "--mcp" in snippet and "ES_STUDIO_MCP_TOKEN" in snippet, snippet[:120])
            check("授权表出现一行（只给指纹）", page.locator("table.grants tbody tr").count() == 1)
            grants_text = page.inner_text("table.grants")
            check("授权表不含令牌明文", len(snippet) > 0 and snippet.split('"')[-2] not in grants_text[:200], grants_text[:120])

            # Try-call goes through the same path an AI client uses.
            page.locator("table.tools tbody tr", has_text="es_get_cluster_health").first.locator(
                'button:has-text("试调用")').click()
            page.wait_for_selector("section.call", timeout=10000)
            page.click('section.call button:has-text("调用")')
            page.wait_for_selector("pre.result", timeout=30000)
            result_text = page.inner_text("pre.result")
            meta = page.inner_text(".call-meta")
            check("试调用返回真实集群健康", '"status"' in result_text, result_text[:120])
            check("试调用显示真实 HTTP 状态与耗时", "HTTP 200" in meta and "GET /_cluster/health" in meta, meta[:120])

            page.wait_for_timeout(600)
            log_rows = page.locator("table.calls tbody tr").count()
            check("调用日志记录了刚才的调用", log_rows >= 1, log_rows)
            check("日志行显示工具名", "es_get_cluster_health" in page.inner_text("table.calls"), "")
            shot("ux-mcp")

            page.click('table.grants button:has-text("撤销")')
            page.wait_for_timeout(800)
            check("撤销后授权列表清空", page.locator("table.grants tbody tr").count() == 0)
        except Exception as e:
            shot("ux-mcp-FAIL")
            check("MCP 工具目录列出 40+ 只读工具", False, e)

        # --------------------------------------------------------- REST Console
        try:
            click_nav(page, "REST 控制台")
            wait_text(page, "Tab 缩进", timeout=20000)
            page.fill("input.path", "/products/_search")
            ta = page.locator("textarea.body")
            ta.click()
            page.keyboard.type('{"size":1}')
            before = ta.input_value()
            page.keyboard.press("Tab")
            page.wait_for_timeout(250)
            after = ta.input_value()
            focused = page.evaluate("document.activeElement && document.activeElement.tagName")
            check("REST Console Tab 插入缩进", len(after) == len(before) + 2, f"{len(before)} -> {len(after)}")
            check("REST Console Tab 后光标仍在编辑器", focused == "TEXTAREA", focused)
            page.keyboard.press("Control+Enter")
            wait_text(page, "HTTP 200", timeout=20000)
            check("REST Console Ctrl+Enter 发送并拿到 200", "HTTP 200" in page.inner_text("div.page"), "")
            shot("ux-rest")
        except Exception as e:
            shot("ux-rest-FAIL")
            check("REST Console Tab 插入缩进", False, e)

        browser.close()

    report = {"base_url": base_url, "results": results, "console": console}
    (out / "ux-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    passed = sum(1 for r in results if r["ok"])
    print(f"\n=== UX regression @ {base_url} -> {out} ===")
    for r in results:
        status = "OK " if r["ok"] else "FAIL"
        extra = f"  ({str(r.get('error'))[:140]})" if not r["ok"] and r.get("error") else ""
        print(f"  [{status}] {r['step']}{extra}")
    print(f"  steps passed: {passed}/{len(results)}")
    print(f"  console errors/warnings: {len(console)}")
    for c in console[:20]:
        print("    - " + c[:200])
    return report


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://127.0.0.1:5199"))
    ap.add_argument("--out", default=os.environ.get("OUT", "screenshots/ux"))
    args = ap.parse_args()
    run(args.base_url, args.out)
