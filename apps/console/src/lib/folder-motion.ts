import type { FolderMotionRecord } from "@weir/ui";
import { guideStage } from "./guide-events";

// Match the guide recorder's bounded, local-only evidence. No names, addresses
// or editor values are included in an animation record.
const records: FolderMotionRecord[] = [];
export const folderMotionEvidence = () => records.slice();
export function recordFolderMotion(record: FolderMotionRecord) {
  records.push(record);
  if (records.length > 200) records.shift();
  window.dispatchEvent(new CustomEvent("weir:folder:trace", { detail: record }));
  guideStage(`folder-${record.stage}`);
}
