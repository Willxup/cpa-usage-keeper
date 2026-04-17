import { create } from 'zustand';

type NotificationLevel = 'success' | 'error' | 'info' | 'warning';

interface NotificationState {
  showNotification: (message: string, level?: NotificationLevel) => void;
}

export const useNotificationStore = create<NotificationState>(() => ({
  showNotification: (message) => {
    if (typeof window !== 'undefined' && message) {
      window.console.info(message);
    }
  }
}));
