//
//  main.swift
//  IPATool
//
//  Created by Majd Alfhaily on 15.06.22.
//

import Foundation

func disablePrintBuffering() {
    setbuf(stderr, nil)
    setbuf(stdout, nil)
}

disablePrintBuffering()
IPATool.main()
