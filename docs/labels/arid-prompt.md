You are the second, independent rater in a mutation-testing study of Go services. Below are 42 mutants that SURVIVED: they compile and no test in the library's suite fails on them. For each, choose exactly one label:

- equivalent: no input or call sequence can make the mutated program behave differently from the original (same results, same errors, no new panics). Performance-only differences do NOT count as behavior.
- useful: behavior can differ, and a reasonable maintainer would write or strengthen a test to catch it (a real gap in the tests: a boundary, an error path a caller can reach, untested API).
- unproductive: behavior can differ, but a maintainer would not write a test for it: performance-only or optimization changes visible only in speed, defensive checks for states the code never reaches, unreachable error paths, debug or log output.

The line marked with > is the mutated line. "_ = x" or "func() bool { _ = x; return true }()" only keeps a variable used so the mutant compiles; read it as the plain replacement (true, false, nil, deletion). Judge from the code shown and your knowledge of Go; if unsure, pick the more likely label and say why in the reason.

Answer with every key exactly once, in order, each with a one-sentence reason naming the concrete input or argument.

### cron#24  chain.go:68  operator ROR
change: `dur > time.Minute`  →  `dur >= time.Minute`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### retryablehttp#62  client.go:229  operator COR
change: `ok`  →  `func() bool { _ = ok; return false }()`
```go
 221  		}
 222  
 223  	case func() (io.Reader, error):
 224  		bodyReader = body
 225  		tmp, err := body()
 226  		if err != nil {
 227  			return nil, 0, err
 228  		}
 229> 		if lr, ok := tmp.(LenReader); ok {
 230  			contentLength = int64(lr.Len())
 231  		}
 232  		if c, ok := tmp.(io.Closer); ok {
 233  			c.Close()
 234  		}
 235  
 236  	// If a regular byte slice, we can read it over and over via new
 237  	// readers
```

### cron#22  chain.go:68  operator ROR
change: `dur > time.Minute`  →  `dur < time.Minute`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### retryablehttp#332  client.go:735  operator SDL
change: `v.Error("request failed", "error", err, "method", req.Method, "url", redactURL(req.URL))`  →  `_ = v`
```go
 727  
 728  		err := doErr
 729  		if respErr != nil {
 730  			err = respErr
 731  		}
 732  		if err != nil {
 733  			switch v := logger.(type) {
 734  			case LeveledLogger:
 735> 				v.Error("request failed", "error", err, "method", req.Method, "url", redactURL(req.URL))
 736  			case Logger:
 737  				v.Printf("[ERR] %s %s request failed: %v", req.Method, redactURL(req.URL), err)
 738  			}
 739  		} else {
 740  			// Call this here to maintain the behavior of logging all requests,
 741  			// even if CheckRetry signals to stop.
 742  			if c.ResponseLogHook != nil {
 743  				// Call the response logger function if provided.
```

### retryablehttp#364  client.go:779  operator SDL
change: `v.Debug("retrying request", "request", desc, "timeout", wait, "remaining", remain)`  →  `_ = v`
```go
 771  		wait := c.Backoff(c.RetryWaitMin, c.RetryWaitMax, i, resp)
 772  		if logger != nil {
 773  			desc := fmt.Sprintf("%s %s", req.Method, redactURL(req.URL))
 774  			if resp != nil {
 775  				desc = fmt.Sprintf("%s (status: %d)", desc, resp.StatusCode)
 776  			}
 777  			switch v := logger.(type) {
 778  			case LeveledLogger:
 779> 				v.Debug("retrying request", "request", desc, "timeout", wait, "remaining", remain)
 780  			case Logger:
 781  				v.Printf("[DEBUG] %s: retrying in %s (%d left)", desc, wait, remain)
 782  			}
 783  		}
 784  		timer := time.NewTimer(wait)
 785  		select {
 786  		case <-req.Context().Done():
 787  			timer.Stop()
```

### retryablehttp#398  client.go:819  operator SDL
change: `err = respErr`  →  `(statement deleted)`
```go
 811  	defer c.HTTPClient.CloseIdleConnections()
 812  
 813  	var err error
 814  	if prepareErr != nil {
 815  		err = prepareErr
 816  	} else if checkErr != nil {
 817  		err = checkErr
 818  	} else if respErr != nil {
 819> 		err = respErr
 820  	} else {
 821  		err = doErr
 822  	}
 823  
 824  	if c.ErrorHandler != nil {
 825  		return c.ErrorHandler(resp, err, attempt)
 826  	}
 827  
```

### cron#30  chain.go:88  operator SDL
change: `logger.Info("skip")`  →  `(statement deleted)`
```go
  80  		var ch = make(chan struct{}, 1)
  81  		ch <- struct{}{}
  82  		return FuncJob(func() {
  83  			select {
  84  			case v := <-ch:
  85  				defer func() { ch <- v }()
  86  				j.Run()
  87  			default:
  88> 				logger.Info("skip")
  89  			}
  90  		})
  91  	}
  92  }
```

### retryablehttp#419  client.go:855  operator SDL
change: `v.Printf("[ERR] error reading response body: %v", err)`  →  `_ = v`
```go
 847  	defer body.Close()
 848  	_, err := io.Copy(io.Discard, io.LimitReader(body, respReadLimit))
 849  	if err != nil {
 850  		if c.logger() != nil {
 851  			switch v := c.logger().(type) {
 852  			case LeveledLogger:
 853  				v.Error("error reading response body", "error", err)
 854  			case Logger:
 855> 				v.Printf("[ERR] error reading response body: %v", err)
 856  			}
 857  		}
 858  	}
 859  }
 860  
 861  // Get is a shortcut for doing a GET request without making a new client.
 862  func Get(url string) (*http.Response, error) {
 863  	return defaultClient.Get(url)
```

### cron#21  chain.go:68  operator COR
change: `dur > time.Minute`  →  `false`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### cron#23  chain.go:68  operator ROR
change: `dur > time.Minute`  →  `dur <= time.Minute`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### cron#132  cron.go:284  operator SDL
change: `c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)`  →  `(statement deleted)`
```go
 276  					c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
 277  				}
 278  
 279  			case newEntry := <-c.add:
 280  				timer.Stop()
 281  				now = c.now()
 282  				newEntry.Next = newEntry.Schedule.Next(now)
 283  				c.entries = append(c.entries, newEntry)
 284> 				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)
 285  
 286  			case replyChan := <-c.snapshot:
 287  				replyChan <- c.entrySnapshot()
 288  				continue
 289  
 290  			case <-c.stop:
 291  				timer.Stop()
 292  				c.logger.Info("stop")
```

### retryablehttp#354  client.go:767  operator COR
change: `doErr == nil`  →  `false`
```go
 759  		// We do this before drainBody because there's no need for the I/O if
 760  		// we're breaking out
 761  		remain := c.RetryMax - i
 762  		if remain <= 0 {
 763  			break
 764  		}
 765  
 766  		// We're going to retry, consume any response to reuse the connection.
 767> 		if doErr == nil {
 768  			c.drainBody(resp.Body)
 769  		}
 770  
 771  		wait := c.Backoff(c.RetryWaitMin, c.RetryWaitMax, i, resp)
 772  		if logger != nil {
 773  			desc := fmt.Sprintf("%s %s", req.Method, redactURL(req.URL))
 774  			if resp != nil {
 775  				desc = fmt.Sprintf("%s (status: %d)", desc, resp.StatusCode)
```

### retryablehttp#404  client.go:830  operator COR
change: `resp != nil`  →  `false`
```go
 822  	}
 823  
 824  	if c.ErrorHandler != nil {
 825  		return c.ErrorHandler(resp, err, attempt)
 826  	}
 827  
 828  	// By default, we close the response body and return an error without
 829  	// returning the response
 830> 	if resp != nil {
 831  		c.drainBody(resp.Body)
 832  	}
 833  
 834  	// this means CheckRetry thought the request was a failure, but didn't
 835  	// communicate why
 836  	if err == nil {
 837  		return nil, fmt.Errorf("%s %s giving up after %d attempt(s)",
 838  			req.Method, redactURL(req.URL), attempt)
```

### retryablehttp#365  client.go:781  operator SDL
change: `v.Printf("[DEBUG] %s: retrying in %s (%d left)", desc, wait, remain)`  →  `_ = v`
```go
 773  			desc := fmt.Sprintf("%s %s", req.Method, redactURL(req.URL))
 774  			if resp != nil {
 775  				desc = fmt.Sprintf("%s (status: %d)", desc, resp.StatusCode)
 776  			}
 777  			switch v := logger.(type) {
 778  			case LeveledLogger:
 779  				v.Debug("retrying request", "request", desc, "timeout", wait, "remaining", remain)
 780  			case Logger:
 781> 				v.Printf("[DEBUG] %s: retrying in %s (%d left)", desc, wait, remain)
 782  			}
 783  		}
 784  		timer := time.NewTimer(wait)
 785  		select {
 786  		case <-req.Context().Done():
 787  			timer.Stop()
 788  			c.HTTPClient.CloseIdleConnections()
 789  			return nil, req.Context().Err()
```

### retryablehttp#293  client.go:678  operator SDL
change: `v.Debug("performing request", "method", req.Method, "url", redactURL(req.URL))`  →  `_ = v`
```go
 670  		}
 671  	})
 672  
 673  	logger := c.logger()
 674  
 675  	if logger != nil {
 676  		switch v := logger.(type) {
 677  		case LeveledLogger:
 678> 			v.Debug("performing request", "method", req.Method, "url", redactURL(req.URL))
 679  		case Logger:
 680  			v.Printf("[DEBUG] %s %s", req.Method, redactURL(req.URL))
 681  		}
 682  	}
 683  
 684  	var resp *http.Response
 685  	var attempt int
 686  	var shouldRetry bool
```

### cron#244  parser.go:100  operator ERR
change: `return nil, fmt.Errorf("provided bad location %s: %v", spec[eq+1:i], err)`  →  `return nil, nil`
```go
  92  
  93  	// Extract timezone if present
  94  	var loc = time.Local
  95  	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
  96  		var err error
  97  		i := strings.Index(spec, " ")
  98  		eq := strings.Index(spec, "=")
  99  		if loc, err = time.LoadLocation(spec[eq+1 : i]); err != nil {
 100> 			return nil, fmt.Errorf("provided bad location %s: %v", spec[eq+1:i], err)
 101  		}
 102  		spec = strings.TrimSpace(spec[i:])
 103  	}
 104  
 105  	// Handle named schedules (descriptors), if configured
 106  	if strings.HasPrefix(spec, "@") {
 107  		if p.options&Descriptor == 0 {
 108  			return nil, fmt.Errorf("parser does not accept descriptors: %v", spec)
```

### cron#550  spec.go:107  operator COR
change: `t.Month() == time.January`  →  `false`
```go
  99  		if !added {
 100  			added = true
 101  			// Otherwise, set the date at the beginning (since the current time is irrelevant).
 102  			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
 103  		}
 104  		t = t.AddDate(0, 1, 0)
 105  
 106  		// Wrapped around.
 107> 		if t.Month() == time.January {
 108  			goto WRAP
 109  		}
 110  	}
 111  
 112  	// Now get a day in that month.
 113  	//
 114  	// NOTE: This causes issues for daylight savings regimes where midnight does
 115  	// not exist.  For example: Sao Paulo has DST that transforms midnight on
```

### retryablehttp#333  client.go:737  operator SDL
change: `v.Printf("[ERR] %s %s request failed: %v", req.Method, redactURL(req.URL), err)`  →  `_ = v`
```go
 729  		if respErr != nil {
 730  			err = respErr
 731  		}
 732  		if err != nil {
 733  			switch v := logger.(type) {
 734  			case LeveledLogger:
 735  				v.Error("request failed", "error", err, "method", req.Method, "url", redactURL(req.URL))
 736  			case Logger:
 737> 				v.Printf("[ERR] %s %s request failed: %v", req.Method, redactURL(req.URL), err)
 738  			}
 739  		} else {
 740  			// Call this here to maintain the behavior of logging all requests,
 741  			// even if CheckRetry signals to stop.
 742  			if c.ResponseLogHook != nil {
 743  				// Call the response logger function if provided.
 744  				switch v := logger.(type) {
 745  				case LeveledLogger:
```

### cron#26  chain.go:68  operator ROR
change: `dur > time.Minute`  →  `dur != time.Minute`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### retryablehttp#76  client.go:272  operator COR
change: `ok`  →  `func() bool { _ = ok; return false }()`
```go
 264  
 265  	// Compat case
 266  	case io.ReadSeeker:
 267  		raw := body
 268  		bodyReader = func() (io.Reader, error) {
 269  			_, err := raw.Seek(0, 0)
 270  			return io.NopCloser(raw), err
 271  		}
 272> 		if lr, ok := raw.(LenReader); ok {
 273  			contentLength = int64(lr.Len())
 274  		}
 275  
 276  	// Read all in so we can reset
 277  	case io.Reader:
 278  		buf, err := io.ReadAll(body)
 279  		if err != nil {
 280  			return nil, 0, err
```

### cron#248  parser.go:100  operator AOR
change: `eq+1`  →  `eq%1`
```go
  92  
  93  	// Extract timezone if present
  94  	var loc = time.Local
  95  	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
  96  		var err error
  97  		i := strings.Index(spec, " ")
  98  		eq := strings.Index(spec, "=")
  99  		if loc, err = time.LoadLocation(spec[eq+1 : i]); err != nil {
 100> 			return nil, fmt.Errorf("provided bad location %s: %v", spec[eq+1:i], err)
 101  		}
 102  		spec = strings.TrimSpace(spec[i:])
 103  	}
 104  
 105  	// Handle named schedules (descriptors), if configured
 106  	if strings.HasPrefix(spec, "@") {
 107  		if p.options&Descriptor == 0 {
 108  			return nil, fmt.Errorf("parser does not accept descriptors: %v", spec)
```

### cron#20  chain.go:68  operator COR
change: `dur > time.Minute`  →  `true`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### retryablehttp#171  client.go:553  operator COR
change: `resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable`  →  `false`
```go
 545  // will perform exponential backoff based on the attempt number and limited
 546  // by the provided minimum and maximum durations.
 547  //
 548  // It also tries to parse Retry-After response header when a http.StatusTooManyRequests
 549  // (HTTP Code 429) is found in the resp parameter. Hence it will return the number of
 550  // seconds the server states it may be ready to process more requests from this client.
 551  func DefaultBackoff(min, max time.Duration, attemptNum int, resp *http.Response) time.Duration {
 552  	if resp != nil {
 553> 		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
 554  			if sleep, ok := parseRetryAfterHeader(resp.Header["Retry-After"]); ok {
 555  				return sleep
 556  			}
 557  		}
 558  	}
 559  
 560  	mult := math.Pow(2, float64(attemptNum)) * float64(min)
 561  	sleep := time.Duration(mult)
```

### cron#529  spec.go:88  operator AOR
change: `t.Year() + 5`  →  `t.Year() * 5`
```go
  80  
  81  	// Start at the earliest possible time (the upcoming second).
  82  	t = t.Add(1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)
  83  
  84  	// This flag indicates whether a field has been incremented.
  85  	added := false
  86  
  87  	// If no time is found within five years, return zero.
  88> 	yearLimit := t.Year() + 5
  89  
  90  WRAP:
  91  	if t.Year() > yearLimit {
  92  		return time.Time{}
  93  	}
  94  
  95  	// Find the first applicable month.
  96  	// If it's this month, then do nothing.
```

### cron#101  cron.go:240  operator SDL
change: `c.logger.Info("start")`  →  `(statement deleted)`
```go
 232  	c.running = true
 233  	c.runningMu.Unlock()
 234  	c.run()
 235  }
 236  
 237  // run the scheduler.. this is private just due to the need to synchronize
 238  // access to the 'running' state variable.
 239  func (c *Cron) run() {
 240> 	c.logger.Info("start")
 241  
 242  	// Figure out the next activation times for each entry.
 243  	now := c.now()
 244  	for _, entry := range c.entries {
 245  		entry.Next = entry.Schedule.Next(now)
 246  		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
 247  	}
 248  
```

### retryablehttp#327  client.go:729  operator ERR
change: `respErr != nil`  →  `respErr == nil`
```go
 721  		// Check if we should continue with retries.
 722  		shouldRetry, checkErr = c.CheckRetry(req.Context(), resp, doErr)
 723  		if !shouldRetry && doErr == nil && req.responseHandler != nil {
 724  			respErr = req.responseHandler(resp)
 725  			shouldRetry, checkErr = c.CheckRetry(req.Context(), resp, respErr)
 726  		}
 727  
 728  		err := doErr
 729> 		if respErr != nil {
 730  			err = respErr
 731  		}
 732  		if err != nil {
 733  			switch v := logger.(type) {
 734  			case LeveledLogger:
 735  				v.Error("request failed", "error", err, "method", req.Method, "url", redactURL(req.URL))
 736  			case Logger:
 737  				v.Printf("[ERR] %s %s request failed: %v", req.Method, redactURL(req.URL), err)
```

### retryablehttp#330  client.go:732  operator COR
change: `err != nil`  →  `false`
```go
 724  			respErr = req.responseHandler(resp)
 725  			shouldRetry, checkErr = c.CheckRetry(req.Context(), resp, respErr)
 726  		}
 727  
 728  		err := doErr
 729  		if respErr != nil {
 730  			err = respErr
 731  		}
 732> 		if err != nil {
 733  			switch v := logger.(type) {
 734  			case LeveledLogger:
 735  				v.Error("request failed", "error", err, "method", req.Method, "url", redactURL(req.URL))
 736  			case Logger:
 737  				v.Printf("[ERR] %s %s request failed: %v", req.Method, redactURL(req.URL), err)
 738  			}
 739  		} else {
 740  			// Call this here to maintain the behavior of logging all requests,
```

### cron#129  cron.go:281  operator SDL
change: `now = c.now()`  →  `(statement deleted)`
```go
 273  					c.startJob(e.WrappedJob)
 274  					e.Prev = e.Next
 275  					e.Next = e.Schedule.Next(now)
 276  					c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
 277  				}
 278  
 279  			case newEntry := <-c.add:
 280  				timer.Stop()
 281> 				now = c.now()
 282  				newEntry.Next = newEntry.Schedule.Next(now)
 283  				c.entries = append(c.entries, newEntry)
 284  				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)
 285  
 286  			case replyChan := <-c.snapshot:
 287  				replyChan <- c.entrySnapshot()
 288  				continue
 289  
```

### retryablehttp#70  client.go:252  operator SDL
change: `contentLength = int64(buf.Len())`  →  `(statement deleted)`
```go
 244  
 245  	// If a bytes.Buffer we can read the underlying byte slice over and
 246  	// over
 247  	case *bytes.Buffer:
 248  		buf := body
 249  		bodyReader = func() (io.Reader, error) {
 250  			return bytes.NewReader(buf.Bytes()), nil
 251  		}
 252> 		contentLength = int64(buf.Len())
 253  
 254  	// We prioritize *bytes.Reader here because we don't really want to
 255  	// deal with it seeking so want it to match here instead of the
 256  	// io.ReadSeeker case.
 257  	case *bytes.Reader:
 258  		snapshot := *body
 259  		bodyReader = func() (io.Reader, error) {
 260  			r := snapshot
```

### cron#354  parser.go:197  operator ROR
change: `options&SecondOptional > 0`  →  `options&SecondOptional >= 0`
```go
 189  		return nil, fmt.Errorf("expected %d to %d fields, found %d: %s", min, max, count, fields)
 190  	}
 191  
 192  	// Populate the optional field if not provided
 193  	if min < max && len(fields) == min {
 194  		switch {
 195  		case options&DowOptional > 0:
 196  			fields = append(fields, defaults[5]) // TODO: improve access to default
 197> 		case options&SecondOptional > 0:
 198  			fields = append([]string{defaults[0]}, fields...)
 199  		default:
 200  			return nil, fmt.Errorf("unknown optional field")
 201  		}
 202  	}
 203  
 204  	// Populate all fields not part of options with their defaults
 205  	n := 0
```

### cron#25  chain.go:68  operator ROR
change: `dur > time.Minute`  →  `dur == time.Minute`
```go
  60  // have the delay logged at Info.
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68> 			if dur := time.Since(start); dur > time.Minute {
  69  				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
```

### cron#120  cron.go:266  operator SDL
change: `c.logger.Info("wake", "now", now)`  →  `(statement deleted)`
```go
 258  		} else {
 259  			timer = time.NewTimer(c.entries[0].Next.Sub(now))
 260  		}
 261  
 262  		for {
 263  			select {
 264  			case now = <-timer.C:
 265  				now = now.In(c.location)
 266> 				c.logger.Info("wake", "now", now)
 267  
 268  				// Run every entry whose next time was less than now
 269  				for _, e := range c.entries {
 270  					if e.Next.After(now) || e.Next.IsZero() {
 271  						break
 272  					}
 273  					c.startJob(e.WrappedJob)
 274  					e.Prev = e.Next
```

### cron#27  chain.go:69  operator SDL
change: `logger.Info("delay", "duration", dur)`  →  `(statement deleted)`
```go
  61  func DelayIfStillRunning(logger Logger) JobWrapper {
  62  	return func(j Job) Job {
  63  		var mu sync.Mutex
  64  		return FuncJob(func() {
  65  			start := time.Now()
  66  			mu.Lock()
  67  			defer mu.Unlock()
  68  			if dur := time.Since(start); dur > time.Minute {
  69> 				logger.Info("delay", "duration", dur)
  70  			}
  71  			j.Run()
  72  		})
  73  	}
  74  }
  75  
  76  // SkipIfStillRunning skips an invocation of the Job if a previous invocation is
  77  // still running. It logs skips to the given logger at Info level.
```

### cron#565  spec.go:125  operator ROR
change: `t.Hour() != 0`  →  `t.Hour() > 0`
```go
 117  	for !dayMatches(s, t) {
 118  		if !added {
 119  			added = true
 120  			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
 121  		}
 122  		t = t.AddDate(0, 0, 1)
 123  		// Notice if the hour is no longer midnight due to DST.
 124  		// Add an hour if it's 23, subtract an hour if it's 1.
 125> 		if t.Hour() != 0 {
 126  			if t.Hour() > 12 {
 127  				t = t.Add(time.Duration(24-t.Hour()) * time.Hour)
 128  			} else {
 129  				t = t.Add(time.Duration(-t.Hour()) * time.Hour)
 130  			}
 131  		}
 132  
 133  		if t.Day() == 1 {
```

### cron#138  cron.go:299  operator SDL
change: `c.logger.Info("removed", "entry", id)`  →  `(statement deleted)`
```go
 291  				timer.Stop()
 292  				c.logger.Info("stop")
 293  				return
 294  
 295  			case id := <-c.remove:
 296  				timer.Stop()
 297  				now = c.now()
 298  				c.removeEntry(id)
 299> 				c.logger.Info("removed", "entry", id)
 300  			}
 301  
 302  			break
 303  		}
 304  	}
 305  }
 306  
 307  // startJob runs the given job in a new goroutine.
```

### retryablehttp#351  client.go:762  operator ROR
change: `remain <= 0`  →  `remain == 0`
```go
 754  
 755  		if !shouldRetry {
 756  			break
 757  		}
 758  
 759  		// We do this before drainBody because there's no need for the I/O if
 760  		// we're breaking out
 761  		remain := c.RetryMax - i
 762> 		if remain <= 0 {
 763  			break
 764  		}
 765  
 766  		// We're going to retry, consume any response to reuse the connection.
 767  		if doErr == nil {
 768  			c.drainBody(resp.Body)
 769  		}
 770  
```

### cron#134  cron.go:292  operator SDL
change: `c.logger.Info("stop")`  →  `(statement deleted)`
```go
 284  				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)
 285  
 286  			case replyChan := <-c.snapshot:
 287  				replyChan <- c.entrySnapshot()
 288  				continue
 289  
 290  			case <-c.stop:
 291  				timer.Stop()
 292> 				c.logger.Info("stop")
 293  				return
 294  
 295  			case id := <-c.remove:
 296  				timer.Stop()
 297  				now = c.now()
 298  				c.removeEntry(id)
 299  				c.logger.Info("removed", "entry", id)
 300  			}
```

### retryablehttp#2  client.go:49  operator AOR
change: `1 * time.Second`  →  `1 - time.Second`
```go
  41  	"sync"
  42  	"time"
  43  
  44  	cleanhttp "github.com/hashicorp/go-cleanhttp"
  45  )
  46  
  47  var (
  48  	// Default retry configuration
  49> 	defaultRetryWaitMin = 1 * time.Second
  50  	defaultRetryWaitMax = 30 * time.Second
  51  	defaultRetryMax     = 4
  52  
  53  	// defaultLogger is the logger provided with defaultClient
  54  	defaultLogger = log.New(os.Stderr, "", log.LstdFlags)
  55  
  56  	// defaultClient is used for performing requests without explicitly making
  57  	// a new client. It is purposely private to avoid modifications.
```

### cron#189  logger.go:68  operator ROR
change: `i > 0`  →  `i >= 0`
```go
  60  // key/values.
  61  func formatString(numKeysAndValues int) string {
  62  	var sb strings.Builder
  63  	sb.WriteString("%s")
  64  	if numKeysAndValues > 0 {
  65  		sb.WriteString(", ")
  66  	}
  67  	for i := 0; i < numKeysAndValues/2; i++ {
  68> 		if i > 0 {
  69  			sb.WriteString(", ")
  70  		}
  71  		sb.WriteString("%v=%v")
  72  	}
  73  	return sb.String()
  74  }
  75  
  76  // formatTimes formats any time.Time values as RFC3339.
```

### retryablehttp#35  client.go:174  operator ERR
change: `return nil, err`  →  `return nil, nil`
```go
 166  		return err
 167  	}
 168  	r.body = bodyReader
 169  	r.ContentLength = contentLength
 170  	if bodyReader != nil {
 171  		r.GetBody = func() (io.ReadCloser, error) {
 172  			body, err := bodyReader()
 173  			if err != nil {
 174> 				return nil, err
 175  			}
 176  			if rc, ok := body.(io.ReadCloser); ok {
 177  				return rc, nil
 178  			}
 179  			return io.NopCloser(body), nil
 180  		}
 181  	} else {
 182  		r.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
```

### retryablehttp#418  client.go:853  operator SDL
change: `v.Error("error reading response body", "error", err)`  →  `_ = v`
```go
 845  // Try to read the response body so we can reuse this connection.
 846  func (c *Client) drainBody(body io.ReadCloser) {
 847  	defer body.Close()
 848  	_, err := io.Copy(io.Discard, io.LimitReader(body, respReadLimit))
 849  	if err != nil {
 850  		if c.logger() != nil {
 851  			switch v := c.logger().(type) {
 852  			case LeveledLogger:
 853> 				v.Error("error reading response body", "error", err)
 854  			case Logger:
 855  				v.Printf("[ERR] error reading response body: %v", err)
 856  			}
 857  		}
 858  	}
 859  }
 860  
 861  // Get is a shortcut for doing a GET request without making a new client.
```

### retryablehttp#54  client.go:219  operator COR
change: `ok`  →  `func() bool { _ = ok; return false }()`
```go
 211  		bodyReader = body
 212  		tmp, err := body()
 213  		if err != nil {
 214  			return nil, 0, err
 215  		}
 216  		if lr, ok := tmp.(LenReader); ok {
 217  			contentLength = int64(lr.Len())
 218  		}
 219> 		if c, ok := tmp.(io.Closer); ok {
 220  			c.Close()
 221  		}
 222  
 223  	case func() (io.Reader, error):
 224  		bodyReader = body
 225  		tmp, err := body()
 226  		if err != nil {
 227  			return nil, 0, err
```