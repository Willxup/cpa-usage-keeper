import type { ReactNode } from 'react';
import { Modal as AntModal } from 'antd';

export interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  width?: number | string;
  className?: string;
  children: ReactNode;
}

export function Modal({ open, onClose, title, width, className, children }: ModalProps) {
  return (
    <AntModal
      open={open}
      onCancel={onClose}
      title={title}
      width={width}
      className={className}
      footer={null}
      destroyOnHidden
      focusable={{ focusTriggerAfterClose: true }}
    >
      <div className="modal-body">{children}</div>
    </AntModal>
  );
}
