import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import CardTable from '../CardTable';
import type { CardColumn } from '../CardTable';

const COLUMNS: CardColumn[] = [
  { key: 'name', label: 'Name', showInCard: true, primary: true },
  { key: 'status', label: 'Status', showInCard: true },
  { key: 'score', label: 'Score', showInCard: true },
  { key: 'internal', label: 'Internal', showInCard: false },
];

const DATA = [
  { id: '1', name: 'Alice', status: 'Active', score: '95', internal: 'x' },
  { id: '2', name: 'Bob', status: 'Inactive', score: '72', internal: 'y' },
];

describe('CardTable', () => {
  it('renders children table in the sm:block container', () => {
    render(
      <CardTable columns={COLUMNS} data={DATA} keyField="id">
        <table data-testid="real-table"><tbody><tr><td>test</td></tr></tbody></table>
      </CardTable>,
    );
    expect(screen.getByTestId('real-table')).toBeInTheDocument();
  });

  it('renders card layout for each data row', () => {
    render(
      <CardTable columns={COLUMNS} data={DATA} keyField="id">
        <table><tbody><tr><td>test</td></tr></tbody></table>
      </CardTable>,
    );
    // Card titles (primary field)
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
  });

  it('shows only showInCard fields in cards (not internal)', () => {
    render(
      <CardTable columns={COLUMNS} data={DATA} keyField="id">
        <table><tbody><tr><td>test</td></tr></tbody></table>
      </CardTable>,
    );
    // showInCard fields appear once per data row (2 rows → 2 "Status" labels)
    expect(screen.getAllByText('Status')).toHaveLength(2);
    expect(screen.getByText('Active')).toBeInTheDocument();
    // Not showing non-card field label as card field
    const internalLabels = screen.queryAllByText('Internal');
    // 'Internal' only appears in the table header (children), not in cards
    expect(internalLabels.length).toBe(0);
  });

  it('"View details" click triggers onRowClick', () => {
    const onClick = vi.fn();
    render(
      <CardTable columns={COLUMNS} data={DATA} keyField="id" onRowClick={onClick}>
        <table><tbody><tr><td>test</td></tr></tbody></table>
      </CardTable>,
    );
    const links = screen.getAllByText('View details');
    expect(links).toHaveLength(2);
    fireEvent.click(links[0]);
    expect(onClick).toHaveBeenCalledWith(DATA[0]);
  });

  it('shows empty state when data is empty', () => {
    render(
      <CardTable columns={COLUMNS} data={[]} keyField="id">
        <table><tbody><tr><td>empty</td></tr></tbody></table>
      </CardTable>,
    );
    expect(screen.getByText('No data available')).toBeInTheDocument();
  });

  it('does not show "View details" when onRowClick is not provided', () => {
    render(
      <CardTable columns={COLUMNS} data={DATA} keyField="id">
        <table><tbody><tr><td>test</td></tr></tbody></table>
      </CardTable>,
    );
    expect(screen.queryByText('View details')).not.toBeInTheDocument();
  });
});
