import './environment'
import { serve } from './protocol'
import { NativeServices } from './services'

serve(process.stdin, process.stdout, new NativeServices()).catch(error => {
  log.error('Gitowl repository service stopped', error)
  process.exitCode = 1
})
