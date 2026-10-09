import React, { type ComponentProps } from 'react';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

export function RequestEventsTestCard({ events, ...props }: Partial<ComponentProps<typeof RequestEventsDetailsCard>> & { events: UsageEvent[] }) {
  return <RequestEventsDetailsCard
    events={events}
    loading={false}
    totalCount={events.length}
    modelOptions={[]}
    sourceOptions={[]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
    {...props}
  />;
}

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();

export const extractTableHeaders = (html: string) => (
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]))
);

export const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tbody\b[^>]*>\s*<tr\b[^>]*>(.*?)<\/tr>/s)![1];
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};
