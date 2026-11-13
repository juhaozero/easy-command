use std::{
    ffi::c_void,
    fs::File,
    io,
    mem::size_of,
    os::windows::{
        ffi::OsStrExt,
        io::{AsRawHandle, FromRawHandle, OwnedHandle},
    },
    path::Path,
    ptr::{null, null_mut},
};
use windows_sys::Win32::{
    Foundation::{SetHandleInformation, HANDLE, HANDLE_FLAG_INHERIT, WAIT_FAILED, WAIT_OBJECT_0},
    Security::SECURITY_ATTRIBUTES,
    System::{
        JobObjects::{
            CreateJobObjectW, JobObjectExtendedLimitInformation, SetInformationJobObject,
            TerminateJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
            JOB_OBJECT_LIMIT_BREAKAWAY_OK, JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
        },
        Pipes::CreatePipe,
        Threading::{
            CreateProcessW, DeleteProcThreadAttributeList, GetExitCodeProcess,
            InitializeProcThreadAttributeList, UpdateProcThreadAttribute, WaitForSingleObject,
            CREATE_NO_WINDOW, EXTENDED_STARTUPINFO_PRESENT, LPPROC_THREAD_ATTRIBUTE_LIST,
            PROCESS_INFORMATION, PROC_THREAD_ATTRIBUTE_HANDLE_LIST, PROC_THREAD_ATTRIBUTE_JOB_LIST,
            STARTF_USESTDHANDLES, STARTUPINFOEXW,
        },
    },
};

pub(super) struct Process {
    handle: OwnedHandle,
    job: OwnedHandle,
    pub pid: u32,
}

impl Process {
    pub fn spawn(path: &Path) -> io::Result<(Self, File, File, File)> {
        let job = kill_on_close_job()?;
        let (child_stdin, stdin) = pipe(false)?;
        let (stdout, child_stdout) = pipe(true)?;
        let (stderr, child_stderr) = pipe(true)?;
        let mut stdio = [
            child_stdin.as_raw_handle(),
            child_stdout.as_raw_handle(),
            child_stderr.as_raw_handle(),
        ];
        let mut jobs = [job.as_raw_handle()];
        // Attribute values must outlive the attribute list.
        let attributes = Attributes::new()?;
        attributes.set(PROC_THREAD_ATTRIBUTE_HANDLE_LIST, &mut stdio)?;
        attributes.set(PROC_THREAD_ATTRIBUTE_JOB_LIST, &mut jobs)?;

        let mut startup = STARTUPINFOEXW::default();
        startup.StartupInfo.cb = size_of::<STARTUPINFOEXW>() as u32;
        startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
        startup.StartupInfo.hStdInput = stdio[0];
        startup.StartupInfo.hStdOutput = stdio[1];
        startup.StartupInfo.hStdError = stdio[2];
        startup.lpAttributeList = attributes.raw;

        let application: Vec<u16> = path.as_os_str().encode_wide().chain([0]).collect();
        let mut info = PROCESS_INFORMATION::default();
        // The absolute application path is supplied explicitly. Windows binds the job
        // during creation and inherits only the three child pipe handles.
        check(unsafe {
            CreateProcessW(
                application.as_ptr(),
                null_mut(),
                null(),
                null(),
                1,
                EXTENDED_STARTUPINFO_PRESENT | CREATE_NO_WINDOW,
                null(),
                null(),
                &startup.StartupInfo,
                &mut info,
            )
        })?;
        // A successful CreateProcessW transfers both handles to the caller.
        let handle = unsafe { OwnedHandle::from_raw_handle(info.hProcess) };
        let _thread = unsafe { OwnedHandle::from_raw_handle(info.hThread) };
        Ok((
            Self {
                handle,
                job,
                pid: info.dwProcessId,
            },
            stdin.into(),
            stdout.into(),
            stderr.into(),
        ))
    }

    pub fn finish(self, wait_ms: u32) -> io::Result<u32> {
        let handle = self.handle.as_raw_handle();
        match unsafe { WaitForSingleObject(handle, wait_ms) } {
            WAIT_OBJECT_0 => {}
            WAIT_FAILED => return Err(io::Error::last_os_error()),
            _ => {
                check(unsafe { TerminateJobObject(self.job.as_raw_handle(), 1) })?;
                if unsafe { WaitForSingleObject(handle, 1000) } != WAIT_OBJECT_0 {
                    return Err(io::Error::other(
                        "Sidecar did not terminate within the wait limit",
                    ));
                }
            }
        }
        let mut code = 0;
        check(unsafe { GetExitCodeProcess(handle, &mut code) })?;
        Ok(code)
    }
}

fn kill_on_close_job() -> io::Result<OwnedHandle> {
    let raw = unsafe { CreateJobObjectW(null(), null()) };
    if raw.is_null() {
        return Err(io::Error::last_os_error());
    }
    let job = unsafe { OwnedHandle::from_raw_handle(raw) };
    let mut limits = JOBOBJECT_EXTENDED_LIMIT_INFORMATION::default();
    limits.BasicLimitInformation.LimitFlags =
        JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | JOB_OBJECT_LIMIT_BREAKAWAY_OK;
    check(unsafe {
        SetInformationJobObject(
            job.as_raw_handle(),
            JobObjectExtendedLimitInformation,
            (&raw const limits).cast(),
            size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32,
        )
    })?;
    Ok(job)
}

fn pipe(parent_reads: bool) -> io::Result<(OwnedHandle, OwnedHandle)> {
    let mut read: HANDLE = null_mut();
    let mut write: HANDLE = null_mut();
    let security = SECURITY_ATTRIBUTES {
        nLength: size_of::<SECURITY_ATTRIBUTES>() as u32,
        lpSecurityDescriptor: null_mut(),
        bInheritHandle: 1,
    };
    check(unsafe { CreatePipe(&mut read, &mut write, &security, 0) })?;
    let read = unsafe { OwnedHandle::from_raw_handle(read) };
    let write = unsafe { OwnedHandle::from_raw_handle(write) };
    let parent = if parent_reads { &read } else { &write };
    check(unsafe { SetHandleInformation(parent.as_raw_handle(), HANDLE_FLAG_INHERIT, 0) })?;
    Ok((read, write))
}

struct Attributes {
    raw: LPPROC_THREAD_ATTRIBUTE_LIST,
    _storage: Vec<usize>,
}

impl Attributes {
    fn new() -> io::Result<Self> {
        let mut bytes = 0;
        // The null-buffer call asks Windows for the required size.
        unsafe { InitializeProcThreadAttributeList(null_mut(), 2, 0, &mut bytes) };
        if bytes == 0 {
            return Err(io::Error::last_os_error());
        }
        let mut storage = vec![0usize; bytes.div_ceil(size_of::<usize>())];
        let raw = storage.as_mut_ptr().cast();
        check(unsafe { InitializeProcThreadAttributeList(raw, 2, 0, &mut bytes) })?;
        Ok(Self {
            raw,
            _storage: storage,
        })
    }

    fn set(&self, kind: u32, value: &mut [HANDLE]) -> io::Result<()> {
        check(unsafe {
            UpdateProcThreadAttribute(
                self.raw,
                0,
                kind as usize,
                value.as_mut_ptr().cast::<c_void>(),
                std::mem::size_of_val(value),
                null_mut(),
                null(),
            )
        })
    }
}

impl Drop for Attributes {
    fn drop(&mut self) {
        unsafe { DeleteProcThreadAttributeList(self.raw) };
    }
}

fn check(result: i32) -> io::Result<()> {
    if result == 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}
