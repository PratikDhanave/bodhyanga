import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import CaseRunner from './pages/CaseRunner';
import RunTrace from './pages/RunTrace';
import Reviews from './pages/Reviews';
import ReviewDetail from './pages/ReviewDetail';
import Tasks from './pages/Tasks';
import Patients from './pages/Patients';
import PanelReport from './pages/PanelReport';
import Agents from './pages/Agents';
import RPM from './pages/RPM';
import Bench from './pages/Bench';
import LLMTraces from './pages/LLMTraces';
import FineTune from './pages/FineTune';
import About from './pages/About';

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/runs" element={<CaseRunner />} />
        <Route path="/runs/:runId" element={<RunTrace />} />
        <Route path="/reviews" element={<Reviews />} />
        <Route path="/reviews/:id" element={<ReviewDetail />} />
        <Route path="/tasks" element={<Tasks />} />
        <Route path="/patients" element={<Patients />} />
        <Route path="/panel" element={<PanelReport />} />
        <Route path="/agents" element={<Agents />} />
        <Route path="/rpm" element={<RPM />} />
        <Route path="/bench" element={<Bench />} />
        <Route path="/llm-traces" element={<LLMTraces />} />
        <Route path="/fine-tune" element={<FineTune />} />
        <Route path="/about" element={<About />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
