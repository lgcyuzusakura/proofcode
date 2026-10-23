import { formatPatch, parsePatch, type StructuredPatch, type StructuredPatchHunk } from "diff";

export type ReviewHunk = { id: string; header: string; lines: string[]; patch: StructuredPatchHunk };
export type ReviewFile = { id: string; path: string; previousPath?: string; raw: string; prefix: string; parsed: StructuredPatch; hunks: ReviewHunk[]; atomicReason: string };
export type ReviewPatch = { original: string; files: ReviewFile[]; error: string };
const displayPath = (path: string) => path.replace(/^[ab]\//, "");

export function reviewPatch(original: string): ReviewPatch {
  const result: ReviewPatch = { original, files: [], error: "" };
  if (!original) return result;
  try {
    if (new TextEncoder().encode(original).length > 8 * 1024 * 1024) throw Error("补丁超过 8 MiB，无法在此审查。");
    if (original.includes("\0") || !original.endsWith("\n")) throw Error("补丁不完整，请等待完整产物或重新加载。");
    const boundaries = [...original.matchAll(/^diff --git /gm)].map(match => match.index!);
    if (!boundaries.length || boundaries[0] !== 0) throw Error("当前仅支持 Git unified diff，请下载原始补丁检查。");
    result.files = boundaries.map((start, index) => {
      const raw = original.slice(start, boundaries[index + 1] ?? original.length), patches = parsePatch(raw);
      if (patches.length !== 1) throw Error("文件边界不完整，请重新加载补丁。");
      const parsed = patches[0];
      if (!parsed.oldFileName || !parsed.newFileName) throw Error("补丁缺少文件路径。");
      const headers = [...raw.matchAll(/^@@ /gm)], prefix = raw.slice(0, headers[0]?.index ?? raw.length);
      const binary = parsed.isBinary || /^GIT binary patch\r?$/m.test(raw);
      const created = parsed.isCreate || parsed.oldFileName === "/dev/null", deleted = parsed.isDelete || parsed.newFileName === "/dev/null";
      const renamed = parsed.isRename || parsed.isCopy || (!created && !deleted && displayPath(parsed.oldFileName) !== displayPath(parsed.newFileName));
      const mode = !!(parsed.oldMode || parsed.newMode);
      const atomicReason = binary ? "二进制变更需整文件选择" : created ? "新增文件需整文件选择" : deleted ? "删除文件需整文件选择" : renamed ? "重命名或复制需整文件选择" : mode ? "文件权限变更需整文件选择" : "";
      if (!parsed.hunks.length && !atomicReason) throw Error("补丁没有完整的文件变更。");
      if (headers.length !== parsed.hunks.length) throw Error("代码块边界无效。");
      if (!binary && prefix.split("\n").some(line => line && !/^(diff --git |index |--- |\+\+\+ |(?:new file|deleted file|old|new) mode |(?:dis)?similarity index |(?:rename|copy) (?:from|to) )/.test(line))) throw Error("补丁包含无法识别的文件头，请下载原始补丁检查。");
      let previousEnd = 0, delta = 0;
      const hunks = parsed.hunks.map((hunk, hunkIndex) => {
        if (![hunk.oldStart, hunk.newStart, hunk.oldLines, hunk.newLines].every(Number.isSafeInteger) || hunk.oldStart < 1 || hunk.newStart < 1 || hunk.oldLines < 0 || hunk.newLines < 0 || hunk.oldStart < previousEnd || hunk.newStart !== hunk.oldStart + delta) throw Error("代码块行号无效或相互重叠。");
        previousEnd = hunk.oldStart + hunk.oldLines; delta += hunk.newLines - hunk.oldLines;
        const chunk = raw.slice(headers[hunkIndex].index!, headers[hunkIndex + 1]?.index ?? raw.length).split("\n");
        const header = chunk.shift()!; chunk.pop();
        if (chunk.length !== hunk.lines.length || chunk.some((line, lineIndex) => line !== hunk.lines[lineIndex] || (!/^[ +\-]/.test(line) && !/^\\ No newline at end of file\r?$/.test(line)))) throw Error("补丁含有截断或无法识别的代码块。");
        return { id: `${index}:${hunkIndex}`, header, lines: [...hunk.lines], patch: hunk };
      });
      const path = displayPath(deleted ? parsed.oldFileName : parsed.newFileName);
      return { id: String(index), path, previousPath: renamed ? displayPath(parsed.oldFileName) : undefined, raw, prefix, parsed, hunks, atomicReason };
    });
  } catch (cause) { result.files = []; result.error = cause instanceof Error ? cause.message : "无法解析补丁。"; }
  return result;
}

export function selectionIds(review: ReviewPatch): string[] {
  return review.files.flatMap(file => file.atomicReason ? [file.id] : file.hunks.map(hunk => hunk.id));
}
export function fileSelectionIds(file: ReviewFile): string[] {
  return file.atomicReason ? [file.id] : file.hunks.map(hunk => hunk.id);
}
export function selectedPatch(review: ReviewPatch, selected: ReadonlySet<string>): string {
  if (review.error) throw Error(review.error);
  const allowed = new Set(selectionIds(review));
  if ([...selected].some(id => !allowed.has(id))) throw Error("补丁已经变化，请重新选择修改。");
  return review.files.map(file => {
    const ids = fileSelectionIds(file), count = ids.filter(id => selected.has(id)).length;
    if (!count) return "";
    if (count === ids.length) return file.raw;
    if (file.atomicReason) throw Error("此变更需要整文件选择。");
    let delta = 0;
    const hunks = file.hunks.filter(hunk => selected.has(hunk.id)).map(hunk => {
      const adjusted = { ...hunk.patch, newStart: hunk.patch.oldStart + delta, lines: [...hunk.lines] };
      delta += hunk.patch.newLines - hunk.patch.oldLines;
      return adjusted;
    });
    // The old/new blob hashes describe the full result, not this selected subset.
    const prefix = file.prefix.replace(/^index .*\n/gm, "");
    return prefix + formatPatch({ ...file.parsed, isGit: false, hunks }, { includeIndex: false, includeUnderline: false, includeFileHeaders: false });
  }).join("");
}
