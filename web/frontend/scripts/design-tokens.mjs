import { readFile, writeFile } from 'node:fs/promises';
import { parse } from 'yaml';

const source = new URL('../../../docs/design/DESIGN.md', import.meta.url);
const output = new URL('../src/lib/design-tokens.generated.ts', import.meta.url);
const markdown = await readFile(source, 'utf8');
const frontmatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(markdown);
if (!frontmatter) throw new Error('DESIGN.md must contain YAML tokens.');
const tokens = parse(frontmatter[1]);
for (const group of ['colors', 'typography', 'spacing', 'rounded', 'components']) {
  if (!tokens[group] || typeof tokens[group] !== 'object') {
    throw new Error(`Missing design token group: ${group}`);
  }
}
const generated = '// Generated from docs/design/DESIGN.md. Run npm run design:generate; do not edit.\n'
  + `export const designTokens = ${JSON.stringify(tokens, null, 2)} as const;\n`;
if (process.argv.includes('--check')) {
  const existing = await readFile(output, 'utf8').catch(() => '');
  if (existing !== generated) {
    console.error('Design tokens are out of date. Run npm run design:generate.');
    process.exitCode = 1;
  } else console.log('Design tokens match DESIGN.md.');
} else {
  await writeFile(output, generated);
  console.log('Generated src/lib/design-tokens.generated.ts');
}
