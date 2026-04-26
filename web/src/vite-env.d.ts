/// <reference types="vite/client" />

declare module '*.module.scss' {
  const classes: Record<string, string>;
  export default classes;
}

declare module '*.module.css' {
  const classes: Record<string, string>;
  export default classes;
}

declare module '*.scss';
declare module '*.css';
declare module '*.png' {
  const src: string;
  export default src;
}
