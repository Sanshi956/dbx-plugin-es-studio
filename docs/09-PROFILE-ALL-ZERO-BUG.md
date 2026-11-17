# 09 · 查询剖析数据全零：根因实证与修复补丁

> 用户反馈：「查询剖析拿到的数据都是 0，看起来没有真实映射上」。
> 本报告用本地真实 ES 6.8.23 / 7.17.29 实例复现，给出精确到行的根因、可直接粘贴的补丁与验证结果。
> 代码零改动，与 `08-UI-AUDIT-AND-FIX-PLAN.md` 的 P0-3（node 列截断）合并执行。

## 1. 结论

**根因**：`services.ts:1660-1666` 把「ES 7.x wrapper 结构」的识别条件写成了「取第一个 key」，而 ES 6.x/7.x 的真实响应第一个 key 是 `id`（值为复合 ID 字符串）。代码随后执行 `node = record("字符串")` 得到空对象，`searches` 为空 → query/rewrite/collector 全部为 0，node/index 也是错的。

**影响范围**：**ES 6.x 和 7.x 全部中招**；只有 8.x/9.x 的扁平结构（带独立 `node_id`/`index`/`shard_id` 字段）能正常工作。代码注释 `services.ts:1621` 写的 "ES 7/8/9 profile query tree" 以及 `:1657-1658` 的 "ES 7.x wrapper" 假设均与实际响应不符。

**这不是数据量问题，也不是超时问题**，是纯粹的字段映射缺陷——所有分片行都返回，但取值的容器对象选错了。

## 2. 实证：真实响应结构对比

在隔离实例上执行 `POST /prof-test/_search {"size":1,"query":{"match_all":{}},"profile":true}`：

| 版本 | `profile.shards[0]` 的 keys | 独立 `node_id`/`index`/`shard_id` | 当前代码行为 |
|------|------------------------------|-----------------------------------|--------------|
| ES 6.8.23 | `id`, `searches`, `aggregations` | **无** | 错把 `id` 当 nodeId，`node={}` → 全 0 |
| ES 7.17.29 | `id`, `searches`, `aggregations`, `fetch` | **无** | 同上，全 0 |
| ES 8.x/9.x | `id`, `node_id`, `index`, `shard_id`, `searches`, ... | 有 | `nodeId` 非空 → `node=wrapped` → 正常 |

ES 6.8 真实片段（`D:/tmp/profile-68.json`）：

```json
{
  "id": "[OI1lfqdRSD6Zyx5gGJPufg][prof-test][0]",
  "searches": [{
    "query": [{ "type": "MatchAllDocsQuery", "time_in_nanos": 423801, "...": "..." }],
    "rewrite_time": 5200,
    "collector": [{ "name": "CancellableCollector", "time_in_nanos": 4400, "children": [...] }]
  }],
  "aggregations": []
}
```

复合 ID 的格式固定为 `[nodeId][index][shard]`，可安全用正则拆解。

## 3. 根因逐行推演（`services.ts:1656-1683`）

```
:1660  let nodeId = str(wrapped.node_id);        // 6.x/7.x: node_id 不存在 → ""
:1662  if (!nodeId) {                              // 进入"7.x wrapper"分支（误判）
:1663    const key = Object.keys(wrapped)[0];      // key = "id"
:1664    nodeId = key;                             // nodeId = "id"（错误）
:1665    node = record(wrapped[key]);              // record("[OI1...][prof-test][0]") → {}
:1667    const searches = node.searches ?? [];     // node={} → searches=[]（致命点）
:1668    const first = record(searches[0]);        // {}
:1679    queryNanos = 0; :1680 rewriteNanos = 0; :1681 collectorNanos = 0;
```

`Node` 列显示 `id…`（被 `QueryProfiler.svelte:136` 截成 10 字符）、`index` 列空、`shard` 列 `#0`（用数组下标兜底）——与用户看到的现象一致。

## 4. 修复补丁（可直接替换 `services.ts:1656-1683` 的 `rawShards.map` 回调）

```ts
const shards: ProfileShard[] = rawShards.map((entry, i) => {
  const wrapped = record(entry);
  const keys = Object.keys(wrapped);

  // ES 6/7/8/9 扁平结构：{ id, searches, ... }（8/9 另有独立 node_id/index/shard_id）
  // 早期 7.x wrapper（若存在）：{ "<nodeId>": { searches: [...] } }
  // 关键：仅在"唯一 key + 该 key 的值确实含 searches 数组"时才走 wrapper，
  //       否则 6/7.x 的 "id" 会被误认成节点 ID。
  let node = wrapped;
  if (
    keys.length === 1 &&
    !Array.isArray(wrapped.searches) &&
    Array.isArray(record(wrapped[keys[0]]).searches)
  ) {
    node = record(wrapped[keys[0]]);
  }

  // 独立字段优先；6/7.x 从 "[nodeId][index][shard]" 复合 ID 还原。
  const idMatch = /^\[([^\]]+)\]\[([^\]]+)\]\[(\d+)\]$/.exec(str(wrapped.id));
  const nodeId = str(wrapped.node_id) || (idMatch ? idMatch[1] : keys[0] || "");
  const index = str(wrapped.index) || (idMatch ? idMatch[2] : "");
  const shardId = wrapped.shard_id !== undefined
    ? num(wrapped.shard_id)
    : (idMatch ? num(idMatch[3]) : i);

  const searches = Array.isArray(node.searches) ? (node.searches as unknown[]) : [];
  const first = record(searches[0]);
  const nodes: ProfileNode[] = [];
  flattenProfile(first.query, 0, nodes);
  const collectorList = Array.isArray(first.collector) ? (first.collector as unknown[]) : [];
  const rootCollector = record(collectorList[0]);
  return {
    node: nodeId,
    index,
    shard: `#${shardId}`,
    // 根 query 节点的 time_in_nanos 已包含 children
    queryNanos: nodes.length ? nodes[0].timeNanos : 0,
    rewriteNanos: num(first.rewrite_time),
    collectorNanos: num(rootCollector.time_in_nanos),
    nodes,
  };
});
```

同步修改注释 `services.ts:1621`：`/** Walks the ES 6/7/8/9 profile query tree ... */`；`:1657-1658` 的版本说明改为上述实际结构。

## 5. 验证结果

用真实 ES 6.8 响应分别在 `D:/tmp/verify-profile.mjs` 里跑当前逻辑与补丁逻辑：

| 指标 | 当前代码 | 修复后 |
|------|----------|--------|
| 首片 node | `"id"` | `OI1lfqdRSD6Zyx5gGJPufg` |
| 首片 index | `""` | `prof-test` |
| 首片 shard | `#0`（下标兜底） | `#0`（从复合 ID 还原） |
| 首片 queryNanos | `0` | `423801` |
| 首片 rewriteNanos | `0` | `5200` |
| 首片 collectorNanos | `0` | `4400` |
| 5 片 queryNanos 合计 | `0` | `2,422,710` ns |

7.x 补丁逻辑同样通过（结构一致）；8.x 扁平结构因保留 `node_id`/`index`/`shard_id` 优先取值，不受影响。

## 6. 配套改进与测试

1. **显示单位**：`QueryProfiler.svelte:83` 的 `ms()` 固定 `(nanos/1e6).toFixed(3)`，修复后真实值如 `0.424 ms` 可读；但 6.x 上 rewrite 常为 `0.005 ms`，建议改自适应单位（ns/µs/ms），避免看起来仍像 0。
2. **单测 fixture**：把 `D:/tmp/profile-68.json` 和 `D:/tmp/profile-717.json` 精简后作为 fixture 入库，断言 `shards[0].queryNanos > 0 && index === "prof-test"`；防止再次回归。
3. **浏览器回归**：`b06-dsl-analyzer-rest-profiler.py` 在 6.8/7.17/8.19 三种实例上各跑一次，断言 profile 表首行 query 列非 `0.000`。
4. **与 08 号报告合并**：数据映射修好后，`QueryProfiler.svelte:136` 的 node ID `slice(0,10)+"…"` 才有意义去改——先修数据（本报告），再修展示（08 号 P0-3），顺序不可颠倒。

## 7. 实施清单

- [ ] 替换 `services.ts:1656-1683` 的映射回调 + 修正 `:1621`/`:1657-1658` 注释
- [ ] 增加 6.8 / 7.17 profile fixture 单测
- [ ] `ms()` 自适应单位（可选，随 08 号 P0-3 一起做）
- [ ] b06 浏览器脚本三版本回归
- [ ] 版本号 bump + 完整回归链（同 07/08 号报告）
