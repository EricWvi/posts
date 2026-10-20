import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  install,
  readManifest,
  selectAsset,
  verifyStaticElf,
} from "./install-pal.mjs";

// Small ELF headers exercise the same architecture and loader checks used on real release files.
function elf(architecture) {
  const bytes = Buffer.alloc(160);
  Buffer.from([0x7f, 0x45, 0x4c, 0x46, 2, 1]).copy(bytes);
  bytes.writeUInt16LE(architecture === "amd64" ? 62 : 183, 18);
  bytes.writeBigUInt64LE(64n, 32);
  bytes.writeUInt16LE(56, 54);
  bytes.writeUInt16LE(1, 56);
  bytes.writeUInt32LE(1, 64);
  return bytes;
}

function manifest(hash) {
  return {
    version: "v1.2.3",
    assets: ["x86_64", "aarch64"].map((cpu) => {
      const target = `${cpu}-unknown-linux-musl`;
      const name = `pal-v1.2.3-${target}.tar.gz`;
      return {
        target,
        name,
        url: `https://github.com/EricWvi/palace/releases/download/v1.2.3/${name}`,
        sha256: hash,
      };
    }),
  };
}

test("both architectures install only the pinned, checksum-verified static binary", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "install-pal-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  for (const architecture of ["amd64", "arm64"]) {
    const binary = elf(architecture);
    await writeFile(join(directory, "pal"), binary);
    const archive = join(directory, "archive.tar.gz");
    execFileSync("tar", ["-czf", archive, "-C", directory, "pal"]);
    const bytes = await readFile(archive);
    const metadata = manifest(createHash("sha256").update(bytes).digest("hex"));
    const requests = [];
    const source = async (url) => {
      requests.push(url);
      if (url.endsWith("/latest.json"))
        return new Response(JSON.stringify(metadata));
      assert.equal(url, selectAsset(metadata, architecture).url);
      return new Response(bytes);
    };
    const destination = join(directory, architecture);
    assert.equal(
      await install(architecture, destination, "v1.2.3", source),
      "v1.2.3",
    );
    assert.deepEqual(await readFile(join(destination, "pal")), binary);
    assert.equal((await stat(join(destination, "pal"))).mode & 0o777, 0o755);
    assert.deepEqual(requests, [
      "https://github.com/EricWvi/palace/releases/download/v1.2.3/latest.json",
      selectAsset(metadata, architecture).url,
    ]);
    metadata.assets.forEach((asset) => {
      asset.sha256 = "0".repeat(64);
    });
    await assert.rejects(
      install(architecture, destination, "v1.2.3", source),
      /SHA-256 mismatch/,
    );
    assert.deepEqual(await readFile(join(destination, "pal")), binary);
  }
});

test("GNU-only, ambiguous, foreign and mismatched release metadata is refused", async () => {
  const valid = manifest("a".repeat(64));
  assert.throws(() => selectAsset(valid, "386"), /Unsupported/);
  for (const mutate of [
    (m) => {
      m.assets = [];
    },
    (m) => {
      m.assets.push(m.assets[0]);
    },
    (m) => {
      m.assets[0].target = "x86_64-unknown-linux-gnu";
    },
    (m) => {
      m.assets[0].url = "https://other.example/pal.tar.gz";
    },
    (m) => {
      m.version = "v../escape";
    },
  ]) {
    const metadata = structuredClone(valid);
    mutate(metadata);
    assert.throws(() => selectAsset(metadata, "amd64"));
  }
  await assert.rejects(
    readManifest("v2", async () => new Response(JSON.stringify(valid))),
    /does not match/,
  );
  await assert.rejects(
    readManifest(
      "latest",
      async () => new Response("missing", { status: 404 }),
    ),
    /HTTP 404/,
  );
});

test("dynamic loaders, shared dependencies, truncated files and wrong CPUs are refused", () => {
  for (const bytes of [Buffer.from("not an ELF"), elf("arm64")]) {
    assert.throws(() => verifyStaticElf(bytes, "amd64"), /static 64-bit/);
  }
  const interpreter = elf("amd64");
  interpreter.writeUInt32LE(3, 64);
  assert.throws(() => verifyStaticElf(interpreter, "amd64"), /static 64-bit/);
  const dynamic = elf("amd64");
  dynamic.writeUInt32LE(2, 64);
  dynamic.writeBigUInt64LE(128n, 72);
  dynamic.writeBigUInt64LE(32n, 96);
  dynamic.writeBigInt64LE(1n, 128);
  assert.throws(() => verifyStaticElf(dynamic, "amd64"), /static 64-bit/);
});
