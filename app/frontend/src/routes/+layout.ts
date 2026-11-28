import { resolve } from '$app/paths';
import { setBase } from '#lib/core/paths.ts';

// Data loads can make API requests before the layout component mounts.
setBase(resolve('/(home)'));

export const ssr = false;
export const prerender = false;
export const trailingSlash = 'never';
