import { useState, useEffect } from 'react';
import './index.css';

const API_BASE = 'http://localhost:8080';

type TaskDef = {
  id: number;
  name: string;
  image: string;
  command: string;
  dependencies: string;
};

export default function App() {
  const [workflows, setWorkflows] = useState<any[]>([]);
  const [message, setMessage] = useState<{type: 'success' | 'error', text: string} | null>(null);

  const [activeTab, setActiveTab] = useState<'builder' | 'yaml'>('builder');
  const [yamlInput, setYamlInput] = useState('');

  const [wfName, setWfName] = useState('my-workflow');
  const [wfGoal, setWfGoal] = useState('MinimizeTime');
  const [tasks, setTasks] = useState<TaskDef[]>([
    { id: 1, name: 'ingest-data', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: '' },
    { id: 2, name: 'clean-data', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: 'ingest-data' },
    { id: 3, name: 'train-model-a', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: 'clean-data' },
    { id: 4, name: 'train-model-b', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: 'clean-data' },
    { id: 5, name: 'train-model-c', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: 'clean-data' },
    { id: 6, name: 'evaluate', image: 'python:3.12-slim', command: 'python -c "print(1)"', dependencies: 'train-model-a, train-model-b, train-model-c' }
  ]);

  const [showSimulation, setShowSimulation] = useState(false);

  const fetchWorkflows = async () => {
    try {
      const res = await fetch(`${API_BASE}/api/workflows`);
      if (res.ok) {
        const data = await res.json();
        setWorkflows(data.items || []);
      }
    } catch (e) {
      console.error("Failed to fetch workflows", e);
    }
  };

  useEffect(() => {
    fetchWorkflows();
    const interval = setInterval(fetchWorkflows, 3000);
    return () => clearInterval(interval);
  }, []);

  const generateYamlFromState = () => {
    let yamlLines = [
      `apiVersion: v1.wannabe.dev/v1`,
      `kind: AdaptiveWorkflow`,
      `metadata:`,
      `  name: ${wfName}`,
      `spec:`,
      `  optimizationGoal: ${wfGoal}`,
      `  maxResources:`,
      `    cpu: "2"`,
      `    memory: "1Gi"`,
      `  tasks:`
    ];

    tasks.forEach(task => {
      yamlLines.push(`    - name: ${task.name}`);
      yamlLines.push(`      image: ${task.image}`);
      
      const cmdClean = task.command.replace(/"/g, '\\"');
      yamlLines.push(`      command: ["sh", "-c", "${cmdClean}"]`);

      if (task.dependencies && task.dependencies.trim() !== '') {
        const deps = task.dependencies.split(',').map(d => `"${d.trim()}"`).join(', ');
        if (deps.length > 0) {
          yamlLines.push(`      dependencies: [${deps}]`);
        }
      }

      yamlLines.push(`      baseResources:`);
      yamlLines.push(`        requests:`);
      yamlLines.push(`          cpu: 200m`);
      yamlLines.push(`          memory: 256Mi`);
    });

    return yamlLines.join('\n');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setMessage(null);
    const finalYaml = activeTab === 'yaml' ? yamlInput : generateYamlFromState();
    try {
      const res = await fetch(`${API_BASE}/api/workflows`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml: finalYaml })
      });
      if (res.ok) {
        setMessage({ type: 'success', text: 'Workflow submitted successfully!' });
        fetchWorkflows();
        if (activeTab === 'yaml') setYamlInput('');
      } else {
        setMessage({ type: 'error', text: `Failed: ${await res.text()}` });
      }
    } catch (e: any) {
      setMessage({ type: 'error', text: e.message || 'Network error' });
    }
  };

  const addTask = () => {
    setTasks([...tasks, {
      id: Date.now(), name: `task-${tasks.length + 1}`, image: 'python:3.12-slim', command: 'echo "hello"', dependencies: ''
    }]);
  };

  const updateTask = (id: number, field: keyof TaskDef, val: string) => {
    setTasks(tasks.map(t => t.id === id ? { ...t, [field]: val } : t));
  };
  const removeTask = (id: number) => {
    if (tasks.length === 1) return;
    setTasks(tasks.filter(t => t.id !== id));
  };

  // -------------------------------------------------------------
  // SIMULATOR LOGIC
  // -------------------------------------------------------------
  const calculateSimulationLayouts = () => {
    const limitLineY = 150; // px (representing memory limit)
    
    // Baseline logic
    let bEndTimes: Record<string, number> = {};
    let bYStacks: Record<number, number> = {};
    let baselineBlocks = tasks.map(t => {
      let deps = t.dependencies ? t.dependencies.split(',').map(d=>d.trim()) : [];
      let startX = deps.length > 0 ? Math.max(...deps.map(d => bEndTimes[d] || 0)) : 0;
      let width = 80; // random mock width for purely visualization
      let height = 65; // random mock memory usage
      
      let startY = bYStacks[startX] || 0;
      bYStacks[startX] = startY + height;
      bEndTimes[t.name] = startX + width;
      
      return { ...t, left: startX, bottom: startY, width, height, crashed: (startY + height > limitLineY) };
    });

    // Adaptive logic (Greedy Bin-packing pushing right across X)
    let aEndTimes: Record<string, number> = {};
    let aMemoryProfile: {start: number, end: number, heightUsed: number}[] = [];
    let adaptiveBlocks: any[] = [];
    
    const getMemAtX = (x: number) => aMemoryProfile.filter(m => m.start <= x && m.end > x).reduce((a,b)=>a+b.heightUsed, 0);

    tasks.forEach(t => {
      let deps = t.dependencies ? t.dependencies.split(',').map(d=>d.trim()) : [];
      let minStartX = deps.length > 0 ? Math.max(...deps.map(d => aEndTimes[d] || 0)) : 0;
      let width = 80;
      let height = 65;
      
      let startX = minStartX;
      while(true) {
        let maxMemInRange = 0;
        for (let ix = startX; ix < startX + width; ix += 10) {
           maxMemInRange = Math.max(maxMemInRange, getMemAtX(ix));
        }
        if (maxMemInRange + height <= limitLineY) {
           break;
        }
        startX += 10;
      }
      
      let bottom = getMemAtX(startX);
      aMemoryProfile.push({ start: startX, end: startX + width, heightUsed: height });
      aEndTimes[t.name] = startX + width;
      
      adaptiveBlocks.push({ ...t, left: startX, bottom, width, height });
    });

    let isCrasher = baselineBlocks.some(b => b.crashed);

    return { baselineBlocks, adaptiveBlocks, limitLineY, isCrasher };
  };

  const sim = showSimulation ? calculateSimulationLayouts() : null;

  return (
    <div className="app-container">
      <header>
        <h1>Adaptive Workflows</h1>
      </header>

      {/* SIMULATOR MODAL */}
      {showSimulation && sim && (
        <div className="modal-overlay" onClick={() => setShowSimulation(false)}>
          <div className="modal-content animate-in" onClick={e => e.stopPropagation()}>
            <button className="close-btn" onClick={() => setShowSimulation(false)}>×</button>
            <h2 className="workflow-title" style={{ marginBottom: '1.5rem', fontSize: '1.5rem' }}>Engine Benchmark Simulation</h2>
            <p style={{ color: 'var(--text-secondary)', marginBottom: '1rem' }}>
              Comparing 2D executions over Time (X-axis) vs Memory Use (Y-axis). 
            </p>

            <div className="chart-container" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '2rem' }}>
              
              {/* Baseline View */}
              <div>
                <h3>1. Baseline Execution (Argo/Native)</h3>
                <div style={{ position: 'relative', marginTop: '-10px' }}>
                  <div className="axis-label-y">Memory Request</div>
                  <div className="cluster-grid">
                    <div className="limit-line" style={{ bottom: `${sim.limitLineY}px`, top: 'auto' }}></div>
                    <div className="limit-label" style={{ top: 'auto', bottom: `${sim.limitLineY + 5}px`, right: '10px' }}>1.0 GiB Limit</div>
                    
                    {sim.baselineBlocks.map((b, i) => (
                      <div 
                        key={i} 
                        className={`task-block ${b.crashed ? 'block-crashed' : 'block-baseline'}`} 
                        style={{ left: `${b.left}px`, bottom: `${b.bottom}px`, width: `${b.width - 2}px`, height: `${b.height}px` }}
                        title={b.name}
                      >
                        {b.name}
                      </div>
                    ))}
                  </div>
                  <div className="axis-label-x">Timeline Execution (Wait 🡒)</div>
                </div>
                <div className="explanatory-text">
                  {sim.isCrasher ? 
                    <span style={{ color: '#ef4444' }}><strong>❌ CRASH IMMINENT:</strong> Native runtimes stack all parallel independent nodes at `Time=0`. This pierces the Cluster Memory Limit line vertically, triggering instant Pod Evictions!</span> :
                    <span><strong>⚠️ WARNING:</strong> While this implicitly fits, Standard Kubernetes holds buffer memory unused and stacks suboptimally.</span>
                  }
                </div>
              </div>

              {/* Adaptive View */}
              <div>
                <h3>2. Adaptive Execution (Our Operator)</h3>
                <div style={{ position: 'relative', marginTop: '-10px' }}>
                  <div className="axis-label-y">Memory Request</div>
                  <div className="cluster-grid">
                    <div className="limit-line" style={{ bottom: `${sim.limitLineY}px`, top: 'auto' }}></div>
                    <div className="limit-label" style={{ top: 'auto', bottom: `${sim.limitLineY + 5}px`, left: '10px', color: '#10b981' }}>Safe Bound Limit</div>
                    
                    {sim.adaptiveBlocks.map((b, i) => (
                      <div 
                        key={i} 
                        className="task-block block-adaptive" 
                        style={{ left: `${b.left}px`, bottom: `${b.bottom}px`, width: `${b.width - 2}px`, height: `${b.height}px` }}
                        title={b.name}
                      >
                        {b.name}
                      </div>
                    ))}
                  </div>
                  <div className="axis-label-x">Timeline Execution (Wait 🡒)</div>
                </div>
                <div className="explanatory-text">
                  <span style={{ color: '#10b981' }}><strong>✅ PERFECTED:</strong> The C++ Optimizer acts like Tetris, elegantly sliding task execution horizontally along the timeline the moment they threaten to exceed total Memory Limits!</span>
                </div>
              </div>

            </div>
          </div>
        </div>
      )}

      {/* Main UI */}
      <div className="glass-panel submit-section animate-in">
        <h2 className="workflow-title">Create Workflow</h2>
        
        <div className="tabs">
          <button 
            type="button"
            className={`tab ${activeTab === 'builder' ? 'active' : ''}`}
            onClick={() => setActiveTab('builder')}
          >
            Visual Builder
          </button>
          <button 
            type="button"
            className={`tab ${activeTab === 'yaml' ? 'active' : ''}`}
            onClick={() => setActiveTab('yaml')}
          >
            Raw YAML
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          {activeTab === 'builder' ? (
            <div className="builder-interface">
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div className="form-group">
                  <label>Workflow Name</label>
                  <input type="text" value={wfName} onChange={e => setWfName(e.target.value)} required />
                </div>
                <div className="form-group">
                  <label>Optimization Goal</label>
                  <select value={wfGoal} onChange={e => setWfGoal(e.target.value)}>
                    <option value="MinimizeTime">MinimizeTime (Fastest Execution)</option>
                    <option value="MinimizeCost">MinimizeCost (Least Compute)</option>
                  </select>
                </div>
              </div>

              <h3 style={{ marginBottom: '1rem', color: 'var(--text-secondary)' }}>Tasks</h3>
              {tasks.map((task, index) => (
                <div key={task.id} className="task-card">
                  <h4>
                    Task {index + 1}
                    {tasks.length > 1 && (
                      <button type="button" className="remove-btn" onClick={() => removeTask(task.id)}>Remove</button>
                    )}
                  </h4>
                  <div className="form-group">
                    <label>Task Name</label>
                    <input type="text" value={task.name} onChange={e => updateTask(task.id, 'name', e.target.value)} required />
                  </div>
                  <div className="form-group">
                    <label>Docker Image</label>
                    <input type="text" value={task.image} onChange={e => updateTask(task.id, 'image', e.target.value)} required />
                  </div>
                  <div className="form-group full-width">
                    <label>Command to Run</label>
                    <input type="text" value={task.command} onChange={e => updateTask(task.id, 'command', e.target.value)} required />
                  </div>
                  <div className="form-group full-width">
                    <label>Dependencies (comma-separated task names)</label>
                    <input type="text" placeholder="e.g. task-1, dataset-prep" value={task.dependencies} onChange={e => updateTask(task.id, 'dependencies', e.target.value)} />
                  </div>
                </div>
              ))}
              
              <button type="button" className="btn-secondary" onClick={addTask}>+ Add Another Task</button>
            </div>
          ) : (
            <textarea
              value={yamlInput}
              onChange={e => setYamlInput(e.target.value)}
              placeholder="Paste your AdaptiveWorkflow YAML here..."
              required={activeTab === 'yaml'}
            />
          )}

          <div style={{ marginTop: '1rem', display: 'flex', alignItems: 'center' }}>
            <button type="submit">Deploy to Cluster</button>
            <button 
              type="button" 
              className="btn-tertiary"
              onClick={() => setShowSimulation(true)}
            >
              📊 Compare Algorithms
            </button>
          </div>
        </form>

        {message && (
          <div className={`notification ${message.type}`}>
            {message.text}
          </div>
        )}
      </div>

      <div className="grid">
        {workflows.map((wf, idx) => (
          <div key={idx} className="glass-panel animate-in" style={{ animationDelay: `${idx * 0.1}s` }}>
            <div className="workflow-header">
              <span className="workflow-title">{wf.metadata.name}</span>
              <span className={`badge ${(wf.status?.phase || 'Pending').toLowerCase()}`}>
                {wf.status?.phase || 'Pending'}
              </span>
            </div>
            
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              <div>Namespace: {wf.metadata.namespace}</div>
              <div>Tasks: {wf.spec.tasks?.length || 0} configured</div>
            </div>

            <ul className="task-list">
              {Object.entries(wf.status?.taskStatuses || {}).map(([taskName, ts]: [string, any]) => (
                <li key={taskName} className="task-item">
                  <span className="task-name">{taskName}</span>
                  <span className={`badge ${ts.phase.toLowerCase()}`}>{ts.phase}</span>
                </li>
              ))}
            </ul>
          </div>
        ))}
        {workflows.length === 0 && (
          <div style={{ color: 'var(--text-secondary)' }}>No workflows running. Create one above to begin.</div>
        )}
      </div>
    </div>
  );
}
