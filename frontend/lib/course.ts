import type { Course, CourseActivity, SegmentationPlan } from "./api";

// Course position is the sole activity authority; flags are never inferred from IDs.
export function activityOf(course: Course | undefined, scenarioId: string): CourseActivity | undefined {
  return course?.modules.flatMap((module) => module.activities).find((activity) => activity.scenario === scenarioId);
}

export function planActivityOf(course: Course | undefined, binding: keyof SegmentationPlan["activities"]): CourseActivity | undefined {
  const id = course?.features.segmentationPlan?.activities[binding];
  return id ? activityOf(course, id) : undefined;
}
