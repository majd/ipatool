//
//  DeviceFamily.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 17.01.22.
//

import Foundation
import ArgumentParser

public enum DeviceFamily: String, ExpressibleByArgument {
    case phone = "iPhone"
    case pad = "iPad"
    case tv = "AppleTV"

    public var defaultValueDescription: String {
        return rawValue
    }
}
