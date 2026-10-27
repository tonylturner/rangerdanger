"use client";

import { useEffect, useState } from "react";
import { curriculumKey, curriculumStorageChangedEvent, type CurriculumScope } from "./curriculum-storage";

// Rerender readers when curriculum values change in this tab or another.
// Same-tab writes use the custom event because browsers only emit `storage`
// events in the other documents sharing the storage area.
export function useCurriculumStorageRevision(scope: CurriculumScope): number {
  const [revision, setRevision] = useState(0);
  const { packageId, revision: curriculumRevision } = scope;

  useEffect(() => {
    const refresh = () => setRevision((current) => current + 1);
    const scopedStorage = { packageId, revision: curriculumRevision };
    const keyPrefix = curriculumKey(scopedStorage, "");
    const onStorage = (event: StorageEvent) => {
      if (event.key === null || event.key.startsWith(keyPrefix)) refresh();
    };

    window.addEventListener(curriculumStorageChangedEvent(scopedStorage), refresh);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener(curriculumStorageChangedEvent(scopedStorage), refresh);
      window.removeEventListener("storage", onStorage);
    };
  }, [packageId, curriculumRevision]);

  return revision;
}
