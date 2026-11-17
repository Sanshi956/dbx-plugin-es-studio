// Fixture regression test for the query-profile parser (docs/09-PROFILE-ALL-ZERO-BUG.md).
// Imports the real source module directly via Node's TS type-stripping so the
// test always exercises production code (no mirrored copy).
//   node --experimental-strip-types --test tests/profile-parse.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const sourceUrl = pathToFileURL(path.join(here, "..", "src", "lib", "profile-parse.ts")).href;
const { mapProfileShards } = await import(sourceUrl);

function load(name) {
  return JSON.parse(fs.readFileSync(path.join(here, "fixtures", name), "utf8"));
}

function shardsOf(name) {
  const payload = load(name);
  return mapProfileShards(payload.profile.shards);
}

test("ES 6.8 composite id is restored and search times are non-zero", () => {
  const shards = shardsOf("profile-6.8.json");
  assert.equal(shards.length, 5);
  const first = shards[0];
  assert.equal(first.node, "OI1lfqdRSD6Zyx5gGJPufg");
  assert.equal(first.index, "prof-test");
  assert.equal(first.shard, "#0");
  assert.ok(first.queryNanos > 0, `queryNanos should be > 0, got ${first.queryNanos}`);
  assert.equal(first.rewriteNanos, 5200);
  assert.equal(first.collectorNanos, 4400);
});

test("ES 6.8 total queryNanos sum matches the verified baseline 2,422,710", () => {
  const shards = shardsOf("profile-6.8.json");
  const sum = shards.reduce((a, s) => a + s.queryNanos, 0);
  assert.equal(sum, 2422710);
});

test("ES 7.17 flat structure also maps non-zero times", () => {
  const shards = shardsOf("profile-7.17.json");
  assert.equal(shards.length, 1);
  const first = shards[0];
  assert.equal(first.node, "1IgxmuBmScCV0zpwspewUg");
  assert.equal(first.index, "prof-test");
  assert.ok(first.queryNanos > 0, `queryNanos should be > 0, got ${first.queryNanos}`);
  assert.equal(first.rewriteNanos, 3500);
  assert.equal(first.collectorNanos, 226900);
});

test("ES 8.19 standalone fields take priority and keep working", () => {
  const shards = shardsOf("profile-8.19.json");
  assert.equal(shards.length, 1);
  const first = shards[0];
  assert.equal(first.node, "Oi8FlatNode");
  assert.equal(first.index, "prof-test");
  assert.equal(first.shard, "#0");
  assert.equal(first.queryNanos, 888000);
  assert.equal(first.rewriteNanos, 9100);
  assert.equal(first.collectorNanos, 7300);
});

test("legacy 7.x wrapper { nodeId: { searches } } is still recognized", () => {
  const wrapped = [{ "legacyNode": { searches: [{ query: [{ type: "TermQuery", time_in_nanos: 1234 }], rewrite_time: 5, collector: [] }] } }];
  const shards = mapProfileShards(wrapped);
  assert.equal(shards.length, 1);
  assert.equal(shards[0].node, "legacyNode");
  assert.equal(shards[0].queryNanos, 1234);
  assert.equal(shards[0].rewriteNanos, 5);
});
