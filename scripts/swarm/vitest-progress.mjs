// Optional diagnostic reporter: preserve file identity even when a run stalls.
// Use alongside Vitest's default reporter; this does not change test outcomes.
function emit(event, module, name) {
  process.stdout.write(`${JSON.stringify({
    swarmTestProgress: event, at: new Date().toISOString(),
    module: module?.moduleId, name,
  })}\n`);
}

export default class SwarmTestProgress {
  onTestModuleQueued(module) { emit('queued', module); }
  onTestModuleStart(module) { emit('start', module); }
  onTestModuleEnd(module) { emit('end', module); }
  onTestCaseReady(test) {
    if (process.env.SWARM_TEST_TRACE_CASES === '1') {
      emit('test-start', test.module, test.fullName);
    }
  }
}
