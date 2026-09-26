import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";

const output = new URL("../internal/web/assets/dist/", import.meta.url);

await rm(output, { force: true, recursive: true });
await mkdir(new URL("fonts/", output), { recursive: true });
await mkdir(new URL("images/", output), { recursive: true });
await mkdir(new URL("images/icons/", output), { recursive: true });
await mkdir(new URL("licenses/", output), { recursive: true });

await cp(
  new URL(
    "../node_modules/@fontsource-variable/schibsted-grotesk/files/schibsted-grotesk-latin-wght-normal.woff2",
    import.meta.url,
  ),
  new URL("fonts/schibsted-grotesk-latin-wght-normal.woff2", output),
);
await cp(
  new URL(
    "../node_modules/@fontsource-variable/schibsted-grotesk/files/schibsted-grotesk-latin-wght-italic.woff2",
    import.meta.url,
  ),
  new URL("fonts/schibsted-grotesk-latin-wght-italic.woff2", output),
);
await cp(
  new URL("../node_modules/@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2", import.meta.url),
  new URL("fonts/jetbrains-mono-latin-wght-normal.woff2", output),
);
await cp(new URL("../docs/Otter02.png", import.meta.url), new URL("images/otter.png", output));

// Temporary PWA icon (see docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md).
// These are finished, pre-generated renditions, copied verbatim like the
// mascot above — no image-processing dependency runs during the build.
for (const name of [
  "icon-192.png",
  "icon-512.png",
  "icon-512-maskable.png",
  "apple-touch-icon-180.png",
  "favicon-16.png",
  "favicon-32.png",
  "favicon.ico",
]) {
  await cp(new URL(`../docs/pwa-icons/${name}`, import.meta.url), new URL(`images/icons/${name}`, output));
}
await cp(
  new URL("../node_modules/@fontsource-variable/schibsted-grotesk/LICENSE", import.meta.url),
  new URL("licenses/schibsted-grotesk-OFL-1.1.txt", output),
);
await cp(
  new URL("../node_modules/@fontsource-variable/jetbrains-mono/LICENSE", import.meta.url),
  new URL("licenses/jetbrains-mono-OFL-1.1.txt", output),
);
await cp(
  new URL("../node_modules/htmx.org/LICENSE", import.meta.url),
  new URL("licenses/htmx-BSD-2-Clause.txt", output),
);
await cp(
  new URL("../node_modules/tailwindcss/LICENSE", import.meta.url),
  new URL("licenses/tailwindcss-MIT.txt", output),
);
await cp(
  new URL("../node_modules/daisyui/LICENSE", import.meta.url),
  new URL("licenses/daisyui-MIT.txt", output),
);

// Phosphor ships Regular and Fill as separate stylesheets whose @font-face
// lists woff2/woff/ttf/svg beside the stylesheet. Only the woff2 faces are
// bundled, so each src is rewritten to the one embedded font under /assets/.
const phosphor = new URL("../node_modules/@phosphor-icons/web/src/", import.meta.url);
let phosphorCSS = "";
for (const [weight, font] of [
  ["regular", "Phosphor"],
  ["fill", "Phosphor-Fill"],
]) {
  await cp(new URL(`${weight}/${font}.woff2`, phosphor), new URL(`fonts/${font}.woff2`, output));
  const css = await readFile(new URL(`${weight}/style.css`, phosphor), "utf8");
  const rewritten = css.replace(/src:[^;]*;/, `src: url("/assets/fonts/${font}.woff2") format("woff2");`);
  if (rewritten === css) throw new Error(`Phosphor ${weight} stylesheet has no @font-face src to rewrite`);
  phosphorCSS += rewritten;
}
await writeFile(new URL("phosphor.css", output), phosphorCSS);
await cp(new URL("../node_modules/@phosphor-icons/web/LICENSE", import.meta.url), new URL("licenses/phosphor-icons-MIT.txt", output));
