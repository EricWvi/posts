import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const releases = "https://github.com/EricWvi/palace/releases";

// Pin both architectures to one release; an older GNU-only release is deliberately refused.
export function selectAsset(manifest, architecture) {
  const cpu = { amd64: "x86_64", arm64: "aarch64" }[architecture];
  if (!cpu) throw new Error(`Unsupported pal architecture: ${architecture}`);
  const version = manifest.version;
  if (typeof version !== "string" || !/^v[\w.-]+$/.test(version)) {
    throw new Error("Invalid pal release version");
  }
  const target = `${cpu}-unknown-linux-musl`;
  const matches = manifest.assets?.filter((asset) => asset.target === target);
  if (matches?.length !== 1)
    throw new Error(
      `Release ${version} must contain exactly one ${target} archive`,
    );
  const asset = matches[0];
  const name = `pal-${version}-${target}.tar.gz`;
  if (
    asset.name !== name ||
    asset.url !== `${releases}/download/${version}/${name}` ||
    !/^[a-fA-F0-9]{64}$/.test(asset.sha256)
  ) {
    throw new Error("Invalid pal archive metadata");
  }
  return asset;
}

// Validate ELF metadata without executing a foreign-architecture binary on the build host.
export function verifyStaticElf(bytes, architecture) {
  const invalid = () => {
    throw new Error(
      "pal must be a static 64-bit Linux ELF for the target architecture",
    );
  };
  if (
    bytes.length < 64 ||
    !bytes.subarray(0, 6).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46, 2, 1]))
  )
    invalid();
  if (bytes.readUInt16LE(18) !== { amd64: 62, arm64: 183 }[architecture])
    invalid();
  const offset = Number(bytes.readBigUInt64LE(32));
  const size = bytes.readUInt16LE(54);
  const count = bytes.readUInt16LE(56);
  if (
    !Number.isSafeInteger(offset) ||
    size < 56 ||
    count === 0 ||
    offset + size * count > bytes.length
  )
    invalid();
  for (let index = 0; index < count; index++) {
    const header = offset + index * size;
    const type = bytes.readUInt32LE(header);
    if (type === 3) invalid(); // PT_INTERP requires a loader absent from distroless/static.
    if (type === 2) {
      const start = Number(bytes.readBigUInt64LE(header + 8));
      const length = Number(bytes.readBigUInt64LE(header + 32));
      if (
        !Number.isSafeInteger(start) ||
        !Number.isSafeInteger(length) ||
        start + length > bytes.length ||
        length % 16 !== 0
      )
        invalid();
      for (let entry = start; entry < start + length; entry += 16) {
        const tag = bytes.readBigInt64LE(entry);
        if (tag === 0n) break;
        if (tag === 1n) invalid(); // DT_NEEDED identifies a shared-library dependency.
      }
    }
  }
}

// Bounded HTTPS fetches preserve normal GitHub asset redirects but refuse a downgrade.
async function download(url, limit, fetchImpl) {
  const response = await fetchImpl(url, {
    signal: AbortSignal.timeout(180_000),
  });
  if (!response.ok || (response.url && !response.url.startsWith("https://"))) {
    throw new Error(`Cannot download pal: HTTP ${response.status}`);
  }
  const chunks = [];
  let length = 0;
  for await (const chunk of response.body) {
    length += chunk.length;
    if (length > limit) throw new Error("pal download exceeds size limit");
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

// Reading latest happens once in release CI; Docker then receives this fixed version for both CPUs.
export async function readManifest(version = "latest", fetchImpl = fetch) {
  if (version !== "latest" && !/^v[\w.-]+$/.test(version))
    throw new Error("Invalid pal version");
  const url =
    version === "latest"
      ? `${releases}/latest/download/latest.json`
      : `${releases}/download/${version}/latest.json`;
  const manifest = JSON.parse(await download(url, 1024 * 1024, fetchImpl));
  selectAsset(manifest, "amd64");
  selectAsset(manifest, "arm64");
  if (version !== "latest" && manifest.version !== version)
    throw new Error("pal release version does not match manifest");
  return manifest;
}

// Only the checked binary is copied into the final image; Node and extraction tools stay here.
export async function install(
  architecture,
  destination,
  version = "latest",
  fetchImpl = fetch,
) {
  const manifest = await readManifest(version, fetchImpl);
  const asset = selectAsset(manifest, architecture);
  const bytes = await download(asset.url, 128 * 1024 * 1024, fetchImpl);
  if (
    createHash("sha256").update(bytes).digest("hex") !==
    asset.sha256.toLowerCase()
  ) {
    throw new Error(`SHA-256 mismatch for ${asset.name}`);
  }
  await mkdir(destination, { recursive: true });
  // Stage beside the destination so rename remains atomic even when /tmp is a different mount.
  const directory = await mkdtemp(join(destination, ".posts-pal-"));
  try {
    const archive = join(directory, "pal.tar.gz");
    await writeFile(archive, bytes);
    const names = execFileSync("tar", ["-tzf", archive], {
      encoding: "utf8",
    }).trim();
    if (names !== "pal") throw new Error("pal archive must contain only pal");
    execFileSync("tar", ["-xzf", archive, "-C", directory, "pal"]);
    const binary = join(directory, "pal");
    if (!(await lstat(binary)).isFile())
      throw new Error("pal archive member must be a regular file");
    verifyStaticElf(await readFile(binary), architecture);
    await chmod(binary, 0o755);
    await rename(binary, join(destination, "pal"));
    return manifest.version;
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const [architecture, destination, version, ...extra] = process.argv.slice(2);
  if (architecture === "--resolve" && !destination) {
    console.log((await readManifest()).version);
  } else {
    if (!architecture || !destination || extra.length)
      throw new Error(
        "Usage: install-pal.mjs <amd64|arm64> <destination> [version] | --resolve",
      );
    console.log(
      `Installed pal ${await install(architecture, destination, version)}`,
    );
  }
}
