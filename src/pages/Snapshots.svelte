<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchRepositories, fetchSnapshots, createRepository, deleteRepository,
    createSnapshot, deleteSnapshot, restoreSnapshot, fetchIndices, isError,
    type RepositoryRow, type SnapshotRow, type IndexRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let repos = $state<RepositoryRow[]>([]);
  let selectedRepo = $state("");
  let snapshots = $state<SnapshotRow[]>([]);
  let loadingRepos = $state(true);
  let loadingSnaps = $state(false);
  let error = $state("");
  let toast = $state("");

  let repoName = $state("");
  let repoLocation = $state("D:/tmp/es-studio/snapshots");
  let snapName = $state("");
  let indices = $state<IndexRow[]>([]);
  // 手填 "products,orders" 是典型的体验坑：这里直接从集群索引里多选。
  let snapIndexList = $state<string[]>([]);
  let includeGlobal = $state(false);
  let renamePattern = $state("");
  let renameReplacement = $state("");
  let formError = $state("");

  function toggleIndex(name: string): void {
    snapIndexList = snapIndexList.includes(name)
      ? snapIndexList.filter((n) => n !== name)
      : [...snapIndexList, name];
  }
  function selectAllIndices(): void {
    snapIndexList = indices.map((ix) => ix.index);
  }

  type Pending =
    | { kind: "repo" }
    | { kind: "snapshot" }
    | { kind: "delete-repo"; repo: RepositoryRow }
    | { kind: "delete-snapshot"; snap: SnapshotRow }
    | { kind: "restore"; snap: SnapshotRow };
  let pending = $state<Pending | null>(null);

  async function loadRepos(): Promise<void> {
    loadingRepos = true;
    error = "";
    try {
      repos = await fetchRepositories(bridge, connectionId);
      if (!selectedRepo && repos.length) selectedRepo = repos[0].name;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
      repos = [];
    } finally {
      loadingRepos = false;
    }
  }
  $effect(() => {
    void loadRepos();
  });

  $effect(() => {
    const id = connectionId;
    if (!id) return;
    void fetchIndices(bridge, id)
      .then((rows) => {
        indices = rows;
      })
      .catch(() => {
        indices = [];
      });
  });

  async function loadSnaps(repo: string): Promise<void> {
    if (!repo) return;
    loadingSnaps = true;
    try {
      const rows = await fetchSnapshots(bridge, connectionId, repo);
      if (selectedRepo === repo) snapshots = rows;
    } catch {
      if (selectedRepo === repo) snapshots = [];
    } finally {
      if (selectedRepo === repo) loadingSnaps = false;
    }
  }
  $effect(() => {
    const repo = selectedRepo;
    if (repo) void loadSnaps(repo);
  });

  function askRepo(): void {
    formError = "";
    if (!repoName.trim()) {
      formError = "请填写仓库名";
      return;
    }
    if (!repoLocation.trim()) {
      formError = "请填写共享文件系统路径（location）";
      return;
    }
    pending = { kind: "repo" };
  }

  function askSnapshot(): void {
    formError = "";
    if (!selectedRepo) {
      formError = "请先选择仓库";
      return;
    }
    if (!snapName.trim()) {
      formError = "请填写快照名";
      return;
    }
    pending = { kind: "snapshot" };
  }

  async function confirm(): Promise<void> {
    const job = pending;
    pending = null;
    if (!job) return;
    if (job.kind === "repo") {
      const res = await createRepository(bridge, connectionId, repoName.trim(), "fs", { location: repoLocation.trim() });
      toast = isError(res) ? `创建仓库失败：${res.error?.message ?? "HTTP " + res.status}` : `已创建仓库 ${repoName.trim()}`;
      if (!isError(res)) {
        selectedRepo = repoName.trim();
        repoName = "";
      }
      await loadRepos();
      return;
    }
    if (job.kind === "snapshot") {
      const res = await createSnapshot(bridge, connectionId, selectedRepo, snapName.trim(), snapIndexList.join(","), includeGlobal);
      toast = isError(res) ? `创建快照失败：${res.error?.message ?? "HTTP " + res.status}` : `已创建快照 ${snapName.trim()}`;
      if (!isError(res)) snapName = "";
      await loadSnaps(selectedRepo);
      return;
    }
    if (job.kind === "delete-repo") {
      const res = await deleteRepository(bridge, connectionId, job.repo.name);
      toast = isError(res) ? `删除仓库失败：${res.error?.message ?? "HTTP " + res.status}` : `已删除仓库 ${job.repo.name}`;
      if (selectedRepo === job.repo.name) {
        selectedRepo = "";
        snapshots = [];
      }
      await loadRepos();
      return;
    }
    if (job.kind === "delete-snapshot") {
      const res = await deleteSnapshot(bridge, connectionId, job.snap.repository, job.snap.snapshot);
      toast = isError(res)
        ? `删除快照失败：${res.error?.message ?? "HTTP " + res.status}`
        : `已删除快照 ${job.snap.snapshot}`;
      await loadSnaps(selectedRepo);
      return;
    }
    if (job.kind === "restore") {
      const res = await restoreSnapshot(
        bridge, connectionId, job.snap.repository, job.snap.snapshot,
        "", renamePattern, renameReplacement,
      );
      toast = isError(res)
        ? `恢复失败：${res.error?.message ?? "HTTP " + res.status}`
        : `已提交恢复 ${job.snap.snapshot}（后台执行，可在 Tasks 里查看进度）`;
      await loadSnaps(selectedRepo);
    }
  }

  function scopeOf(): string {
    if (!pending) return "";
    switch (pending.kind) {
      case "repo": return `新建文件系统仓库 ${repoName} → ${repoLocation}`;
      case "snapshot": return `仓库 ${selectedRepo} 快照 ${snapName}（索引 ${snapIndexList.join(",") || "*"}）`;
      case "delete-repo": return `仓库 ${pending.repo.name}（其下所有快照会一并不可访问）`;
      case "delete-snapshot": return `仓库 ${pending.snap.repository} 快照 ${pending.snap.snapshot}（${pending.snap.indices.length} 索引）`;
      case "restore": return `从 ${pending.snap.repository}/${pending.snap.snapshot} 恢复 ${pending.snap.indices.length} 个索引${renamePattern ? `，重命名为 ${renamePattern} → ${renameReplacement || "restored_$1"}` : "（原索引名，已存在会被拒绝）"}`;
      default: return "";
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Snapshots</h1>
      <p>Repositories and snapshots from <code>/_snapshot</code>; restore is registered as a cluster task.</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Snapshot API error" detail={error} />{/if}

  <section class="block">
    <h2>Repositories <span class="count">{repos.length}</span></h2>
    <div class="fields">
      <label>仓库名<input placeholder="p3-fs-repo" bind:value={repoName} /></label>
      <label class="wide">location<input bind:value={repoLocation} /></label>
      <button type="button" class="primary" onclick={askRepo}>创建 fs 仓库</button>
    </div>
    {#if formError}<p class="form-error">{formError}</p>{/if}

    {#if loadingRepos}
      <StateBlock kind="loading" message="Reading /_snapshot…" />
    {:else if repos.length === 0}
      <p class="empty">（没有已注册的快照仓库；先用上面的表单创建一个 fs 仓库）</p>
    {:else}
      <table>
        <thead><tr><th>Name</th><th>Type</th><th>Settings</th><th></th></tr></thead>
        <tbody>
          {#each repos as repo (repo.name)}
            <tr class:active={selectedRepo === repo.name}>
              <td class="mono">{repo.name}</td>
              <td>{repo.type}</td>
              <td class="mono small">{Object.entries(repo.settings).map(([k, v]) => `${k}=${v}`).join(", ")}</td>
              <td class="act">
                <button type="button" class="mini" onclick={() => (selectedRepo = repo.name)}>查看快照</button>
                <button type="button" class="mini danger" onclick={() => (pending = { kind: "delete-repo", repo })}>Delete</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  {#if selectedRepo}
    <section class="block">
      <h2>Snapshots in <code>{selectedRepo}</code> <span class="count">{snapshots.length}</span></h2>
      <div class="fields">
        <label>快照名<input placeholder={`snap-${new Date().toISOString().slice(0, 10)}`} bind:value={snapName} /></label>
        <label class="check"><input type="checkbox" bind:checked={includeGlobal} /> include_global_state</label>
        <button type="button" class="primary" onclick={askSnapshot}>创建快照</button>
      </div>

      <div class="ix-picker">
        <div class="ix-head">
          <span>索引</span>
          <span class="muted">不选 = 全部 *</span>
          <span class="ix-count">{snapIndexList.length} / {indices.length}</span>
          <button type="button" class="mini" onclick={selectAllIndices}>全选</button>
          <button type="button" class="mini" onclick={() => (snapIndexList = [])}>清空</button>
        </div>
        <div class="ix-chips">
          {#each indices as ix (ix.index)}
            <label class="chip" class:on={snapIndexList.includes(ix.index)}>
              <input
                type="checkbox"
                checked={snapIndexList.includes(ix.index)}
                onchange={() => toggleIndex(ix.index)}
              />
              {ix.index}
            </label>
          {:else}
            <span class="muted">（没有可选的索引）</span>
          {/each}
        </div>
      </div>
      <div class="fields">
        <label>restore rename_pattern<input placeholder="(.+)" bind:value={renamePattern} /></label>
        <label>rename_replacement<input placeholder="restored_$1" bind:value={renameReplacement} /></label>
        <span class="hint">留空则按原索引名恢复（同名已存在会被拒绝）</span>
      </div>

      {#if loadingSnaps}
        <StateBlock kind="loading" message={`Reading /_snapshot/${selectedRepo}/_all…`} />
      {:else if snapshots.length === 0}
        <p class="empty">（该仓库下没有快照）</p>
      {:else}
        <table>
          <thead><tr><th>Snapshot</th><th>State</th><th>Indices</th><th>Shards ok/total</th><th>Started</th><th>Duration</th><th></th></tr></thead>
          <tbody>
            {#each snapshots as snap (snap.snapshot)}
              <tr>
                <td class="mono">{snap.snapshot}</td>
                <td><span class="state {snap.state}">{snap.state || "-"}</span></td>
                <td class="small">{snap.indices.length}（{snap.indices.slice(0, 3).join(", ")}{snap.indices.length > 3 ? "…" : ""}）</td>
                <td class="mono">{snap.successful}/{snap.total}{snap.failed ? ` · failed ${snap.failed}` : ""}</td>
                <td class="mono small">{snap.startTime ? new Date(Number(snap.startTime)).toLocaleString() : "-"}</td>
                <td class="mono small">{snap.duration ? `${(Number(snap.duration) / 1000).toFixed(1)}s` : "-"}</td>
                <td class="act">
                  <button type="button" class="mini" onclick={() => (pending = { kind: "restore", snap })}>Restore</button>
                  <button type="button" class="mini danger" onclick={() => (pending = { kind: "delete-snapshot", snap })}>Delete</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
</div>

<ConfirmDanger
  open={pending !== null}
  title={pending?.kind === "restore" ? "恢复快照"
    : pending?.kind === "delete-snapshot" || pending?.kind === "delete-repo" ? "删除快照数据"
    : "写入快照配置"}
  message={pending?.kind === "restore"
    ? "恢复会写入索引数据与状态；同名索引已存在时会被拒绝（除非设置 rename），恢复错误可能覆盖别名指向。"
    : pending?.kind === "delete-snapshot" || pending?.kind === "delete-repo"
      ? "删除后备份文件将从仓库中移除，无法通过本插件找回。"
      : "仓库路径必须对所有 master/data 节点一致可写，否则快照会失败并可能残留不完整分片文件。"}
  scope={scopeOf()}
  confirmLabel={pending?.kind === "restore" ? "确认恢复" : "确认执行"}
  onConfirm={confirm}
  onCancel={() => (pending = null)}
/>

<style>
  .page { max-width: 1140px; }
  header { margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .block { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .block h2 { margin: 0 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .block h2 code { font-family: var(--es-mono); font-size: 11px; letter-spacing: 0; text-transform: none; }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields .wide { flex: 1 1 220px; }
  .fields .check { flex-direction: row; align-items: center; gap: 6px; padding-bottom: 6px; }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); width: 100%; }
  .fields .check input { width: auto; }

  .ix-picker { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 8px 10px 10px; margin-bottom: 10px; background: var(--es-surface-2); }
  .ix-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; font-size: 11px; color: var(--es-muted); }
  .ix-head .ix-count { font-family: var(--es-mono); }
  .ix-head button { padding: 2px 8px; font-size: 11px; }
  .ix-chips { display: flex; flex-wrap: wrap; gap: 6px; max-height: 112px; overflow: auto; }
  .chip { display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px 3px 7px; border: 1px solid var(--es-border); border-radius: 999px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: 11px; cursor: pointer; user-select: none; }
  .chip:hover { border-color: var(--es-accent); }
  .chip.on { background: var(--es-accent); border-color: var(--es-accent); color: #fff; }
  .chip input { appearance: none; width: 12px; height: 12px; margin: 0; border: 1px solid currentColor; border-radius: 3px; background: transparent; flex: 0 0 auto; }
  .chip.on input { background: #fff; box-shadow: inset 0 0 0 3px var(--es-accent); border-color: #fff; }
  .hint { font-size: 11px; color: var(--es-muted); padding-bottom: 8px; }
  .form-error { margin: 0 0 10px; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: 11px; color: var(--es-muted); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
  .state { font-size: 11px; }
  .state.SUCCESS { color: var(--es-green); }
  .state.FAILED, .state.PARTIAL { color: var(--es-red); }
  .state.IN_PROGRESS { color: var(--es-amber, #b45309); }
  .empty { margin: 0; font-size: 11px; color: var(--es-muted); }
</style>
