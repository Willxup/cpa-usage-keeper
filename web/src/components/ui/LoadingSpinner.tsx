import { Spin } from 'antd';

export function LoadingSpinner({
  size = 20,
  className = ''
}: {
  size?: number;
  className?: string;
}) {
  return (
    <span className={className} role="status" aria-live="polite">
      <Spin size={size <= 18 ? 'small' : size >= 32 ? 'large' : 'default'} />
    </span>
  );
}
