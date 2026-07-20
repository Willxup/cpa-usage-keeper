import { createContext, useContext } from 'react';

/**
 * Whether the app is running inside the CPAMC host iframe (`?embed=cpamc`).
 * Provided once by App; pages consume it to collapse their shell into the
 * compact embed variant instead of rendering full chrome.
 */
export const EmbedContext = createContext(false);

export const useEmbed = (): boolean => useContext(EmbedContext);
