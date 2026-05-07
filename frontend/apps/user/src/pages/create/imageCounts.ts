export const IMAGE_COUNT_OPTIONS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] as const;

export type ImageCountOption = (typeof IMAGE_COUNT_OPTIONS)[number];
