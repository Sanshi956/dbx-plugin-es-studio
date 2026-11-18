<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchRepositories, fetchSnapshots, createRepository, deleteRepository,
    createSnapshot, deleteSnapshot, restoreSnapshot, fetchIndices, isError,
    type RepositoryRow, type SnapshotRow, type IndexRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

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
      formError = t("snap.err.noRepoName");
      return;
    }
    if (!repoLocation.trim()) {
      formError = t("snap.err.noLocation");
      return;
    }
    pending = { kind: "repo" };
  }

  function askSnapshot(): void {
    formError = "";
    if (!selectedRepo) {
      formError = t("snap.err.noRepoSelected");
      return;
    }
    if (!snapName.trim()) {
      formError = t("snap.err.noSnapName");
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
      toast = isError(res)
        ? t("snap.toast.createRepoFailed", { detail: res.error?.message ?? "HTTP " + res.status })
        : t("snap.toast.repoCreated", { name: repoName.trim() });
      if (!isError(res)) {
        selectedRepo = repoName.trim();
        repoName = "";
      }
      await loadRepos();
      return;
    }
    if (job.kind === "snapshot") {
      const res = await createSnapshot(bridge, connectionId, selectedRepo, snapName.trim(), snapIndexList.join(","), includeGlobal);
      toast = isError(res)
        ? t("snap.toast.createSnapFailed", { detail: res.error?.message ?? "HTTP " + res.status })
        : t("snap.toast.snapCreated", { name: snapName.trim() });
      if (!isError(res)) snapName = "";
      await loadSnaps(selectedRepo);
      return;
    }
    if (job.kind === "delete-repo") {
      const res = await deleteRepository(bridge, connectionId, job.repo.name);
      toast = isError(res)
        ? t("snap.toast.deleteRepoFailed", { detail: res.error?.message ?? "HTTP " + res.status })
        : t("snap.toast.repoDeleted", { name: job.repo.name });
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
        ? t("snap.toast.deleteSnapFailed", { detail: res.error?.message ?? "HTTP " + res.status })
        : t("snap.toast.snapDeleted", { name: job.snap.snapshot });
      await loadSnaps(selectedRepo);
      return;
    }
    if (job.kind === "restore") {
      const res = await restoreSnapshot(
        bridge, connectionId, job.snap.repository, job.snap.snapshot,
        "", renamePattern, renameReplacement,
      );
      toast = isError(res)
        ? t("snap.toast.restoreFailed", { detail: res.error?.message ?? "HTTP " + res.status })
        : t("snap.toast.restored", { name: job.snap.snapshot });
      await loadSnaps(selectedRepo);
    }
  }

  function scopeOf(): string {
    if (!pending) return "";
    switch (pending.kind) {
      case "repo": return t("snap.scope.repo", { name: repoName, location: repoLocation });
      case "snapshot":
        return t("snap.scope.snapshot", { repo: selectedRepo, name: snapName, indices: snapIndexList.join(",") || "*" });
      case "delete-repo": return t("snap.scope.deleteRepo", { name: pending.repo.name });
      case "delete-snapshot":
        return t("snap.scope.deleteSnapshot", {
          repo: pending.snap.repository,
          name: pending.snap.snapshot,
          count: pending.snap.indices.length,
        });
      case "restore":
        return renamePattern
          ? t("snap.scope.restoreRenamed", {
              repo: pending.snap.repository,
              snapshot: pending.snap.snapshot,
              count: pending.snap.indices.length,
              pattern: renamePattern,
              replacement: renameReplacement || "restored_$1",
            })
          : t("snap.scope.restoreOriginal", {
              repo: pending.snap.repository,
              snapshot: pending.snap.snapshot,
              count: pending.snap.indices.length,
            });
      default: return "";
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("snap.title")}</h1>
      <p>{t("snap.hintPrefix")}<code>/_snapshot</code>{t("snap.hintMid")}</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("snap.apiError")} detail={error} />{/if}

  <section class="block">
    <h2>{t("snap.repos")} <span class="count">{repos.length}</span></h2>
    <div class="fields">
      <label>{t("snap.repoName")}<input placeholder="fs-repo" bind:value={repoName} /></label>
      <label class="wide">location<input bind:value={repoLocation} /></label>
      <button type="button" class="primary" onclick={askRepo}>{t("snap.createRepo")}</button>
    </div>
    {#if formError}<p class="form-error">{formError}</p>{/if}

    {#if loadingRepos}
      <StateBlock kind="loading" message={t("snap.reading")} />
    {:else if repos.length === 0}
      <p class="empty">{t("snap.noRepos")}</p>
    {:else}
      <table>
        <thead><tr><th>{t("snap.col.name")}</th><th>{t("snap.col.type")}</th><th>{t("snap.col.settings")}</th><th></th></tr></thead>
        <tbody>
          {#each repos as repo (repo.name)}
            <tr class:active={selectedRepo === repo.name}>
              <td class="mono">{repo.name}</td>
              <td>{repo.type}</td>
              <td class="mono small">{Object.entries(repo.settings).map(([k, v]) => `${k}=${v}`).join(", ")}</td>
              <td class="act">
                <button type="button" class="mini" onclick={() => (selectedRepo = repo.name)}>{t("snap.viewSnapshots")}</button>
                <button type="button" class="mini danger" onclick={() => (pending = { kind: "delete-repo", repo })}>{t("common.delete")}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  {#if selectedRepo}
    <section class="block">
      <h2>{t("snap.snapshotsIn", { repo: selectedRepo })} <span class="count">{snapshots.length}</span></h2>
      <div class="fields">
        <label>{t("snap.snapName")}<input placeholder={`snap-${new Date().toISOString().slice(0, 10)}`} bind:value={snapName} /></label>
        <label class="check"><input type="checkbox" bind:checked={includeGlobal} /> include_global_state</label>
        <button type="button" class="primary" onclick={askSnapshot}>{t("snap.createSnapshot")}</button>
      </div>

      <div class="ix-picker">
        <div class="ix-head">
          <span>{t("snap.indicesLabel")}</span>
          <span class="muted">{t("snap.indicesNone")}</span>
          <span class="ix-count">{snapIndexList.length} / {indices.length}</span>
          <button type="button" class="mini" onclick={selectAllIndices}>{t("snap.selectAll")}</button>
          <button type="button" class="mini" onclick={() => (snapIndexList = [])}>{t("snap.clear")}</button>
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
            <span class="muted">{t("snap.noIndices")}</span>
          {/each}
        </div>
      </div>
      <div class="fields">
        <label>restore rename_pattern<input placeholder="(.+)" bind:value={renamePattern} /></label>
        <label>rename_replacement<input placeholder="restored_$1" bind:value={renameReplacement} /></label>
        <span class="hint">{t("snap.renameHint")}</span>
      </div>

      {#if loadingSnaps}
        <StateBlock kind="loading" message={t("snap.readingIn", { repo: selectedRepo })} />
      {:else if snapshots.length === 0}
        <p class="empty">{t("snap.noSnapshots")}</p>
      {:else}
        <table>
          <thead><tr><th>{t("snap.col.snapshot")}</th><th>{t("snap.col.state")}</th><th>{t("snap.col.indices")}</th><th>{t("snap.col.shards")}</th><th>{t("snap.col.started")}</th><th>{t("snap.col.duration")}</th><th></th></tr></thead>
          <tbody>
            {#each snapshots as snap (snap.snapshot)}
              <tr>
                <td class="mono">{snap.snapshot}</td>
                <td><span class="state {snap.state}">{snap.state || "-"}</span></td>
                <td class="small" title={snap.indices.join(", ")}>{snap.indices.length}（{snap.indices.slice(0, 3).join(", ")}{snap.indices.length > 3 ? "…" : ""}）</td>
                <td class="mono">{snap.successful}/{snap.total}{snap.failed ? ` · failed ${snap.failed}` : ""}</td>
                <td class="mono small">{snap.startTime ? new Date(Number(snap.startTime)).toLocaleString() : "-"}</td>
                <td class="mono small">{snap.duration ? `${(Number(snap.duration) / 1000).toFixed(1)}s` : "-"}</td>
                <td class="act">
                  <button type="button" class="mini" onclick={() => (pending = { kind: "restore", snap })}>{t("snap.restore")}</button>
                  <button type="button" class="mini danger" onclick={() => (pending = { kind: "delete-snapshot", snap })}>{t("common.delete")}</button>
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
  title={pending?.kind === "restore"
    ? t("snap.confirm.restoreTitle")
    : pending?.kind === "delete-snapshot" || pending?.kind === "delete-repo"
      ? t("snap.confirm.deleteTitle")
      : t("snap.confirm.writeTitle")}
  message={pending?.kind === "restore"
    ? t("snap.confirm.restoreMessage")
    : pending?.kind === "delete-snapshot" || pending?.kind === "delete-repo"
      ? t("snap.confirm.deleteMessage")
      : t("snap.confirm.writeMessage")}
  scope={scopeOf()}
  confirmLabel={pending?.kind === "restore" ? t("snap.confirm.restoreLabel") : t("snap.confirm.defaultLabel")}
  onConfirm={confirm}
  onCancel={() => (pending = null)}
/>

<style>
  .page { max-width: 1140px; }
  header { margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .block { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .block h2 { margin: 0 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields .wide { flex: 1 1 220px; }
  .fields .check { flex-direction: row; align-items: center; gap: 6px; padding-bottom: 6px; }
  .fields input { width: 100%; }
  .fields .check input { width: auto; }

  .ix-picker { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 8px 10px 10px; margin-bottom: 10px; background: var(--es-surface-2); }
  .ix-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .ix-head .ix-count { font-family: var(--es-mono); }
  .ix-head button { padding: 2px 8px; font-size: var(--es-fs-xs); }
  .ix-chips { display: flex; flex-wrap: wrap; gap: 6px; max-height: 112px; overflow: auto; }
  .chip { display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px 3px 7px; border: 1px solid var(--es-border); border-radius: 999px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: var(--es-fs-xs); cursor: pointer; user-select: none; }
  .chip:hover { border-color: var(--es-accent); }
  .chip.on { background: var(--es-accent); border-color: var(--es-accent); color: #fff; }
  .chip input { appearance: none; width: 12px; height: 12px; margin: 0; border: 1px solid currentColor; border-radius: 3px; background: transparent; flex: 0 0 auto; }
  .chip.on input { background: #fff; box-shadow: inset 0 0 0 3px var(--es-accent); border-color: #fff; }
  .hint { font-size: var(--es-fs-xs); color: var(--es-muted); padding-bottom: 8px; }
  .form-error { margin: 0 0 10px; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
  .state { font-size: var(--es-fs-xs); }
  .state.SUCCESS { color: var(--es-green); }
  .state.FAILED, .state.PARTIAL { color: var(--es-red); }
  .state.IN_PROGRESS { color: var(--es-amber, #b45309); }
  .empty { margin: 0; font-size: var(--es-fs-xs); color: var(--es-muted); }
</style>
