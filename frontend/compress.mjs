import { readdir, readFile, writeFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";
for (const name of await readdir("../web/assets")) {
  if (/\.(js|css)$/.test(name)) {
    const source = await readFile("../web/assets/" + name);
    await writeFile(
      "../web/assets/" + name + ".gz",
      gzipSync(source, { level: 9 }),
    );
  }
}
