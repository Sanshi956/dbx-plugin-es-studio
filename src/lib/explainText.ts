/*
 * Translation helpers for `GET /_cluster/allocation/explain` output (0.5.1).
 *
 * ES's explanations are free-form English; the decider NAMES and decision
 * VALUES however are a finite enum, so they get dictionary treatment. The
 * long explanations get best-effort pattern translation for the messages we
 * actually see in practice; anything unmatched stays verbatim (machine
 * output, shown as-is rather than mistranslated).
 */
import { getLocale, tOr } from "./i18n.svelte.js";

/** yes / no / worse_balance / always — the values ES actually returns. */
export function decisionLabel(value: string): string {
  if (!value) return "—";
  return tOr(`sdiag.decision.${value}`, value);
}

/** Decider name → dictionary label; unknown deciders stay verbatim. */
export function deciderLabel(name: string): string {
  if (!name) return "—";
  return tOr(`sdiag.decider.${name}`, name);
}

/**
 * Pattern table for the recurring explanation templates. Keyword matching on
 * purpose: ES interpolates node/index names into these sentences, so exact
 * regexes would rot across versions. First match wins.
 */
const REASON_PATTERNS: Array<[RegExp, string]> = [
  [/a copy of this shard is already allocated to this node/i, "该节点上已存在本分片的副本（同分片冲突）。"],
  [/a copy of this shard is already allocated to a different node/i, "本分片已分配到其他节点。"],
  [/shard is already allocated to this node/i, "该节点上已存在本分片副本。"],
  [/no copy of this shard is expected to be allocated on this node/i, "按预期本分片不会分配到该节点（过滤规则或节点属性不匹配）。"],
  [/because of include filters|allocation filtering|allocation rules/i, "不满足索引的 include/exclude 过滤规则。"],
  [/exceeds the flood-stage watermark/i, "磁盘使用率超过洪水水位线。"],
  [/exceeds the low watermark/i, "磁盘使用率超过低水位线。"],
  [/exceeds the high watermark/i, "磁盘使用率超过高水位线。"],
  [/node has insufficient disk/i, "节点磁盘剩余空间不足。"],
  [/waiting for .*primary.* to be allocated|replica.*will only be allocated after/i, "副本分片等待对应主分片先分配完成。"],
  [/temporary delay|delayed allocation|waiting for the cluster to be ready/i, "节点离开集群触发的延迟分配计时中。"],
  [/allocation is disabled|shard allocation has been disabled/i, "集群分片分配功能当前被禁用。"],
  [/does not match .* allocation (filter|rules)|node.*not match/i, "节点属性与索引分配过滤规则不匹配。"],
  [/no attributes? matching|attribute .* missing/i, "没有满足索引要求的节点属性。"],
  [/version .* incompatible|node version/i, "节点 ES 版本与索引要求不兼容。"],
  [/index is (red|yellow)|primary shard.*not allocated/i, "索引存在未分配的主分片。"],
  [/shard.*recover|recovery.*in progress/i, "分片恢复尚未完成。"],
];

/** zh 环境下做模式翻译；en 环境原文即英文，原样返回。 */
export function explanationText(raw: string): string {
  if (!raw || getLocale() !== "zh-CN") return raw;
  for (const [pattern, text] of REASON_PATTERNS) {
    if (pattern.test(raw)) return text;
  }
  return raw;
}
