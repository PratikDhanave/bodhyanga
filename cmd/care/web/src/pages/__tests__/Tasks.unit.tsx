import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import Tasks from '../Tasks';
import { mockFetch } from '../../test/setup';
import type { NurseTask } from '../../types';

const TASKS: NurseTask[] = [
  { id: 't1', kind: 'follow_up', patient_id: 'p1', priority: 'urgent', status: 'open', subject: 'Call patient about labs', due_at: '2026-06-07T10:00:00Z', created_at: '2026-06-06T08:00:00Z' },
  { id: 't2', kind: 'med_review', patient_id: 'p2', priority: 'normal', status: 'done', subject: 'Review discharge meds', due_at: '2026-06-08T14:00:00Z', created_at: '2026-06-06T09:00:00Z' },
  { id: 't3', kind: 'referral', patient_id: 'p3', priority: 'high', status: 'open', subject: 'Schedule cardiology referral', due_at: '2026-06-09T12:00:00Z', created_at: '2026-06-06T10:00:00Z' },
];

function renderTasks() {
  return render(
    <MemoryRouter>
      <Tasks />
    </MemoryRouter>,
  );
}

describe('Tasks', () => {
  it('renders the task list with all tasks', async () => {
    mockFetch(() => TASKS);
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    expect(view.getByText('Call patient about labs')).toBeInTheDocument();
    expect(view.getByText('Review discharge meds')).toBeInTheDocument();
    expect(view.getByText('Schedule cardiology referral')).toBeInTheDocument();
  });

  it('shows empty state when no tasks', async () => {
    mockFetch(() => []);
    renderTasks();
    expect(await screen.findByText('No tasks')).toBeInTheDocument();
  });

  it('filters by urgent when Urgent tab is clicked', async () => {
    mockFetch(() => TASKS);
    const user = userEvent.setup();
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    await view.findByText('Call patient about labs');
    await user.click(screen.getByRole('tab', { name: /Urgent/ }));

    expect(view.getByText('Call patient about labs')).toBeInTheDocument();
    expect(view.queryByText('Review discharge meds')).not.toBeInTheDocument();
    expect(view.queryByText('Schedule cardiology referral')).not.toBeInTheDocument();
  });

  it('filters by open when Open tab is clicked', async () => {
    mockFetch(() => TASKS);
    const user = userEvent.setup();
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    await view.findByText('Call patient about labs');
    await user.click(screen.getByRole('tab', { name: /Open/ }));

    expect(view.getByText('Call patient about labs')).toBeInTheDocument();
    expect(view.getByText('Schedule cardiology referral')).toBeInTheDocument();
    expect(view.queryByText('Review discharge meds')).not.toBeInTheDocument();
  });

  it('shows all tasks when All tab is clicked after filtering', async () => {
    mockFetch(() => TASKS);
    const user = userEvent.setup();
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    await view.findByText('Call patient about labs');
    await user.click(screen.getByRole('tab', { name: /Urgent/ }));
    await user.click(screen.getByRole('tab', { name: /All/ }));

    expect(view.getByText('Call patient about labs')).toBeInTheDocument();
    expect(view.getByText('Review discharge meds')).toBeInTheDocument();
  });

  it('renders priority badges correctly', async () => {
    mockFetch(() => TASKS);
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    const urgentBadge = view.getByText('urgent');
    expect(urgentBadge.className).toContain('badge-urgent');
  });

  it('displays subtitle with count', async () => {
    mockFetch(() => TASKS);
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    await within(table).findByText('Call patient about labs');
    expect(screen.getByText('3 of 3')).toBeInTheDocument();
  });

  it('shows overdue tab with count', async () => {
    mockFetch(() => TASKS);
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    await within(table).findByText('Call patient about labs');
    const overdueTab = screen.getByRole('tab', { name: /Overdue/ });
    expect(overdueTab).toBeInTheDocument();
  });

  it('renders sortable column headers', async () => {
    mockFetch(() => TASKS);
    const user = userEvent.setup();
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    await view.findByText('Call patient about labs');
    // Click Priority header to sort by priority
    const priorityHeader = view.getByText(/^Priority/);
    await user.click(priorityHeader);
    expect(priorityHeader.textContent).toContain('↓');
  });

  it('sorts by due date by default', async () => {
    mockFetch(() => TASKS);
    renderTasks();

    const table = await screen.findByRole('table', { name: 'Nurse tasks' });
    const view = within(table);
    await view.findByText('Call patient about labs');
    // Due header should show ↓ since it's the default sort
    const dueHeader = view.getByText(/^Due/);
    expect(dueHeader.textContent).toContain('↓');
  });
});
